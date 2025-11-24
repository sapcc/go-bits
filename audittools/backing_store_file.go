// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/majewsky/gg/option"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/logg"
)

// NewFileBackingStore creates a file-based backing store from JSON parameters.
// This is the factory function for use in AuditorOpts.BackingStoreFactories.
//
// Example usage:
//
//	auditor, err := NewAuditor(ctx, AuditorOpts{
//	    BackingStoreFactories: map[string]BackingStoreFactory{
//	        "file": NewFileBackingStore,
//	    },
//	})
func NewFileBackingStore(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
	var store FileBackingStore
	if err := json.Unmarshal(params, &store); err != nil {
		return nil, fmt.Errorf("audittools: failed to parse file backing store config: %w", err)
	}

	registry := opts.Registry
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	if err := store.Init(registry); err != nil {
		return nil, err
	}
	return &store, nil
}

// FileBackingStore implements BackingStore using local filesystem files.
// Provides durable audit buffering for services with persistent volumes.
//
// Thread safety: Write() and ReadBatch() are serialized by a mutex.
// Multiple concurrent calls are safe but will block each other.
// Callers must ensure that commit() completes before the next ReadBatch() call.
type FileBackingStore struct {
	// Configuration (JSON params)
	Directory    string               `json:"directory"`
	MaxFileSize  option.Option[int64] `json:"max_file_size"`
	MaxTotalSize option.Option[int64] `json:"max_total_size"`

	// Runtime state (not from JSON)
	mu              sync.Mutex   `json:"-"`
	currentFile     string       `json:"-"`
	cachedTotalSize atomic.Int64 `json:"-"`

	// Metrics (initialized in Init)
	writeCounter prometheus.Counter     `json:"-"`
	readCounter  prometheus.Counter     `json:"-"`
	errorCounter *prometheus.CounterVec `json:"-"`
	sizeGauge    prometheus.Gauge       `json:"-"`
	fileGauge    prometheus.Gauge       `json:"-"`
}

// Init implements BackingStore.
func (s *FileBackingStore) Init(registry prometheus.Registerer) error {
	if s.Directory == "" {
		return errors.New("audittools: directory is required for file backing store")
	}

	// 10 MB per file balances write performance (fewer rotations) with memory usage during reads.
	// 0 = unlimited total size allows unbounded growth during extended RabbitMQ outages.
	if s.MaxFileSize.IsNone() {
		s.MaxFileSize = option.Some[int64](10 * 1024 * 1024)
	}
	if s.MaxTotalSize.IsNone() {
		s.MaxTotalSize = option.Some[int64](0)
	}

	// 0700 permissions prevent other users from reading audit data.
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return fmt.Errorf("audittools: failed to create directory: %w", err)
	}

	s.initializeMetrics(registry)
	s.initializeCachedSize()

	return nil
}

func (s *FileBackingStore) initializeMetrics(registry prometheus.Registerer) {
	s.writeCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "audittools_backing_store_writes_total",
		Help: "Total number of audit events written to the backing store.",
	})
	s.readCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "audittools_backing_store_reads_total",
		Help: "Total number of audit events read from the backing store.",
	})
	s.errorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "audittools_backing_store_errors_total",
		Help: "Total number of errors encountered by the backing store.",
	}, []string{"operation"})
	s.sizeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "audittools_backing_store_size_bytes",
		Help: "Current total size of the backing store in bytes.",
	})
	s.fileGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "audittools_backing_store_files_count",
		Help: "Current number of files in the backing store.",
	})

	if registry != nil {
		registry.MustRegister(s.writeCounter, s.readCounter, s.errorCounter, s.sizeGauge, s.fileGauge)
	}
}

// initializeCachedSize calculates the initial total size from existing files.
// Enables fast size checks during Write() without filesystem calls on every operation.
func (s *FileBackingStore) initializeCachedSize() {
	files, err := s.listFiles()
	if err != nil {
		return
	}

	totalSize := s.calculateTotalSize(files)
	s.cachedTotalSize.Store(totalSize)
}

// Write implements BackingStore.
func (s *FileBackingStore) Write(event cadf.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetFile, err := s.getCurrentOrRotatedFile()
	if err != nil {
		return err
	}
	s.currentFile = targetFile

	// Enforce size limit before write to prevent unbounded growth during extended outages.
	if maxTotalSize, ok := s.MaxTotalSize.Unpack(); ok && maxTotalSize > 0 {
		if err := s.checkTotalSizeLimit(maxTotalSize); err != nil {
			s.errorCounter.WithLabelValues("write_full").Inc()
			return fmt.Errorf("audittools: failed to write to backing store: %w", err)
		}
	}

	eventSize, err := s.writeEventToFile(targetFile, event)
	if err != nil {
		return err
	}

	s.cachedTotalSize.Add(eventSize)
	s.writeCounter.Inc()
	return nil
}

// ReadBatch implements BackingStore.
func (s *FileBackingStore) ReadBatch() ([]cadf.Event, func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := s.listFiles()
	if err != nil {
		return nil, nil, err
	}

	if len(files) == 0 {
		return nil, nil, nil
	}

	oldest := files[0]
	if oldest == s.currentFile {
		// Clear current file to force rotation on next write, preventing simultaneous read/write to same file.
		s.currentFile = ""
	}

	events, err := s.readEventsFromFile(oldest)
	if err != nil {
		return nil, nil, err
	}

	commit := s.makeCommitFunc(oldest)

	s.readCounter.Add(float64(len(events)))
	return events, commit, nil
}

// UpdateMetrics implements BackingStore.
func (s *FileBackingStore) UpdateMetrics() error {
	files, err := s.listFiles()
	if err != nil {
		return err
	}

	totalSize := s.calculateTotalSize(files)

	// Synchronize cached size with filesystem to correct any drift from incomplete writes or external modifications.
	s.cachedTotalSize.Store(totalSize)

	s.sizeGauge.Set(float64(totalSize))
	s.fileGauge.Set(float64(len(files)))
	return nil
}

// Close implements BackingStore.
func (s *FileBackingStore) Close() error {
	return nil
}

func (s *FileBackingStore) getCurrentOrRotatedFile() (string, error) {
	if s.currentFile == "" {
		return s.newFileName(), nil
	}

	if needsRotation, err := s.needsRotation(s.currentFile); err != nil {
		return "", err
	} else if needsRotation {
		return s.newFileName(), nil
	}

	return s.currentFile, nil
}

func (s *FileBackingStore) needsRotation(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		s.errorCounter.WithLabelValues("write_stat").Inc()
		return false, fmt.Errorf("audittools: failed to stat backing store file: %w", err)
	}
	maxFileSize := s.MaxFileSize.UnwrapOr(10 * 1024 * 1024)
	return info.Size() >= maxFileSize, nil
}

// newFileName generates a new file name with unique timestamp.
// Nanosecond precision ensures uniqueness even with rapid rotation.
func (s *FileBackingStore) newFileName() string {
	return filepath.Join(s.Directory, fmt.Sprintf("audit-events-%d.jsonl", time.Now().UnixNano()))
}

// writeEventToFile writes a CADF event to the specified file with fsync.
// fsync ensures audit data survives system crashes, as required for compliance.
func (s *FileBackingStore) writeEventToFile(filePath string, event cadf.Event) (int64, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		s.errorCounter.WithLabelValues("write_open").Inc()
		return 0, fmt.Errorf("audittools: failed to open backing store file: %w", err)
	}

	b, err := json.Marshal(event)
	if err != nil {
		f.Close()
		s.errorCounter.WithLabelValues("write_marshal").Inc()
		return 0, fmt.Errorf("audittools: failed to marshal event: %w", err)
	}

	eventSize := int64(len(b) + 1)

	_, err = f.Write(append(b, '\n'))
	if err != nil {
		f.Close()
		s.errorCounter.WithLabelValues("write_io").Inc()
		return 0, fmt.Errorf("audittools: failed to write to backing store: %w", err)
	}

	// fsync required for audit compliance - data must survive system crashes.
	if err := f.Sync(); err != nil {
		f.Close()
		s.errorCounter.WithLabelValues("write_sync").Inc()
		return 0, fmt.Errorf("audittools: failed to sync backing store file: %w", err)
	}

	if err := f.Close(); err != nil {
		s.errorCounter.WithLabelValues("write_close").Inc()
		return 0, fmt.Errorf("audittools: failed to close backing store file: %w", err)
	}

	return eventSize, nil
}

func (s *FileBackingStore) checkTotalSizeLimit(maxTotalSize int64) error {
	currentSize := s.cachedTotalSize.Load()
	if currentSize < maxTotalSize {
		return nil
	}
	return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, currentSize, maxTotalSize)
}

// readEventsFromFile reads all events from a file, handling corrupted entries.
// Corrupted events are moved to dead-letter files rather than discarded to preserve audit data.
func (s *FileBackingStore) readEventsFromFile(path string) ([]cadf.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		s.errorCounter.WithLabelValues("read_open").Inc()
		return nil, fmt.Errorf("audittools: failed to open backing store file: %w", err)
	}
	defer f.Close()

	// Preallocate for estimated 100 events per file (10MB max / ~100KB per event).
	events := make([]cadf.Event, 0, 100)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e cadf.Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			s.handleCorruptedEvent(scanner.Bytes(), path)
			continue
		}
		events = append(events, e)
	}

	if err := scanner.Err(); err != nil {
		s.errorCounter.WithLabelValues("read_scan").Inc()
		return nil, fmt.Errorf("audittools: failed to scan backing store file: %w", err)
	}

	return events, nil
}

func (s *FileBackingStore) makeCommitFunc(path string) func() error {
	return func() error {
		fileSize := getFileSize(path)

		if err := os.Remove(path); err != nil {
			s.errorCounter.WithLabelValues("commit_remove").Inc()
			return err
		}

		if fileSize > 0 {
			s.cachedTotalSize.Add(-fileSize)
		}

		return nil
	}
}

// writeDeadLetter writes a corrupted event to a dead-letter file for manual investigation.
// Preserves corrupted audit data for forensic analysis rather than silent data loss.
func (s *FileBackingStore) writeDeadLetter(corruptedLine []byte, sourceFile string) error {
	// Timestamp-based naming allows multiple dead-letter files for rotation and cleanup.
	deadLetterFile := filepath.Join(s.Directory, fmt.Sprintf("audit-events-deadletter-%d.jsonl", time.Now().UnixNano()))

	f, err := os.OpenFile(deadLetterFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("audittools: failed to open dead-letter file: %w", err)
	}

	// Include metadata alongside corrupted data to enable investigation and recovery.
	entry := struct {
		Timestamp  string `json:"timestamp"`
		SourceFile string `json:"source_file"`
		RawData    string `json:"raw_data"`
		Error      string `json:"error"`
	}{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		SourceFile: filepath.Base(sourceFile),
		RawData:    string(corruptedLine),
		Error:      "failed to unmarshal event",
	}

	b, err := json.Marshal(entry)
	if err != nil {
		f.Close()
		return fmt.Errorf("audittools: failed to marshal dead-letter entry: %w", err)
	}

	_, err = f.Write(append(b, '\n'))
	if err != nil {
		f.Close()
		return fmt.Errorf("audittools: failed to write to dead-letter file: %w", err)
	}

	// fsync dead-letter files - corrupted audit data still requires durability guarantees.
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("audittools: failed to sync dead-letter file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("audittools: failed to close dead-letter file: %w", err)
	}

	s.errorCounter.WithLabelValues("deadletter_write").Inc()
	return nil
}

// listFiles returns all event files in the backing store directory, sorted by name.
// Sorted by name ensures FIFO processing since filenames contain timestamps.
func (s *FileBackingStore) listFiles() ([]string, error) {
	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		return nil, fmt.Errorf("audittools: failed to read backing store directory: %w", err)
	}

	// Preallocate capacity based on directory entries to avoid reallocations.
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if isEventFile(entry) {
			files = append(files, filepath.Join(s.Directory, entry.Name()))
		}
	}

	slices.Sort(files)
	return files, nil
}

func (s *FileBackingStore) calculateTotalSize(files []string) int64 {
	var totalSize int64
	for _, f := range files {
		totalSize += getFileSize(f)
	}
	return totalSize
}

func (s *FileBackingStore) handleCorruptedEvent(corruptedLine []byte, sourceFile string) {
	if err := s.writeDeadLetter(corruptedLine, sourceFile); err != nil {
		logg.Error("audittools: failed to write to dead-letter file: %s", err.Error())
		s.errorCounter.WithLabelValues("deadletter_write_failed").Inc()
	}
	s.errorCounter.WithLabelValues("corrupted_event").Inc()
}

// isEventFile returns true if the entry is a regular event file (not a directory or dead-letter file).
// Excludes dead-letter files from normal processing to prevent reprocessing corrupted data.
func isEventFile(entry os.DirEntry) bool {
	if entry.IsDir() {
		return false
	}

	name := entry.Name()
	return strings.HasPrefix(name, "audit-events-") &&
		!strings.Contains(name, "deadletter") &&
		strings.HasSuffix(name, ".jsonl")
}

// getFileSize returns the size of the file, or 0 if it cannot be determined.
// Returns 0 on error to allow size calculations to continue rather than fail entirely.
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
