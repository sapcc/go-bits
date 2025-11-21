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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/logg"
)

func init() {
	// Self-register with the backing store registry
	BackingStoreRegistry.Add(func() BackingStore {
		return &FileBackingStore{}
	})
}

// FileBackingStore implements BackingStore using local filesystem files.
//
// Thread safety: Write() and ReadBatch() are serialized by a mutex.
// Multiple concurrent calls are safe but will block each other.
// Callers must ensure that commit() is called and completes before the next ReadBatch() call.
type FileBackingStore struct {
	// Configuration (JSON params)
	Directory    string `json:"directory"`
	MaxFileSize  int64  `json:"max_file_size"`
	MaxTotalSize int64  `json:"max_total_size"`

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

// PluginTypeID implements pluggable.Plugin.
func (s *FileBackingStore) PluginTypeID() string {
	return "file"
}

// Init implements BackingStore.
func (s *FileBackingStore) Init(registry prometheus.Registerer) error {
	// Validate configuration
	if s.Directory == "" {
		return errors.New("audittools: directory is required for file backing store")
	}

	// Set defaults
	if s.MaxFileSize == 0 {
		s.MaxFileSize = 10 * 1024 * 1024 // 10 MB
	}

	// Create directory
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return fmt.Errorf("audittools: failed to create directory: %w", err)
	}

	s.initializeMetrics(registry)
	s.initializeCachedSize()

	return nil
}

// initializeMetrics creates and registers Prometheus metrics.
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

	// Determine target file (rotate if needed)
	targetFile, err := s.getCurrentOrRotatedFile()
	if err != nil {
		return err
	}
	s.currentFile = targetFile

	// Check capacity limit if configured
	if s.MaxTotalSize > 0 {
		if err := s.checkTotalSizeLimit(); err != nil {
			s.errorCounter.WithLabelValues("write_full").Inc()
			return fmt.Errorf("audittools: failed to write to backing store: %w", err)
		}
	}

	// Write event to file
	eventSize, err := s.writeEventToFile(targetFile, event)
	if err != nil {
		return err
	}

	// Update cached size after successful write
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
		s.currentFile = "" // Rotate to avoid race with Write
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

	// Update cached size with actual filesystem size to fix any drift
	s.cachedTotalSize.Store(totalSize)

	s.sizeGauge.Set(float64(totalSize))
	s.fileGauge.Set(float64(len(files)))
	return nil
}

// Close implements BackingStore.
func (s *FileBackingStore) Close() error {
	return nil
}

// getCurrentOrRotatedFile returns the file to write to, creating a new one if needed.
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

// needsRotation checks if the current file needs rotation.
func (s *FileBackingStore) needsRotation(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		s.errorCounter.WithLabelValues("write_stat").Inc()
		return false, fmt.Errorf("audittools: failed to stat backing store file: %w", err)
	}
	return info.Size() >= s.MaxFileSize, nil
}

// newFileName generates a new file name with unique timestamp.
func (s *FileBackingStore) newFileName() string {
	return filepath.Join(s.Directory, fmt.Sprintf("audit-events-%d.jsonl", time.Now().UnixNano()))
}

// writeEventToFile writes a CADF event to the specified file with fsync.
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

	// Force flush to disk for maximum durability
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

// checkTotalSizeLimit verifies that the backing store hasn't exceeded its size limit.
func (s *FileBackingStore) checkTotalSizeLimit() error {
	currentSize := s.cachedTotalSize.Load()
	if currentSize < s.MaxTotalSize {
		return nil
	}
	return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, currentSize, s.MaxTotalSize)
}

// readEventsFromFile reads all events from a file, handling corrupted entries.
func (s *FileBackingStore) readEventsFromFile(path string) ([]cadf.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		s.errorCounter.WithLabelValues("read_open").Inc()
		return nil, fmt.Errorf("audittools: failed to open backing store file: %w", err)
	}
	defer f.Close()

	var events []cadf.Event
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

// makeCommitFunc creates a commit function for removing a file after successful processing.
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
func (s *FileBackingStore) writeDeadLetter(corruptedLine []byte, sourceFile string) error {
	// Dead-letter files are named with a timestamp to allow rotation and cleanup
	deadLetterFile := filepath.Join(s.Directory, fmt.Sprintf("audit-events-deadletter-%d.jsonl", time.Now().UnixNano()))

	f, err := os.OpenFile(deadLetterFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("audittools: failed to open dead-letter file: %w", err)
	}

	// Write metadata with the corrupted line for investigation
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

	// Force flush to disk for dead-letter files (critical audit data)
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
func (s *FileBackingStore) listFiles() ([]string, error) {
	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		return nil, fmt.Errorf("audittools: failed to read backing store directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if isEventFile(entry) {
			files = append(files, filepath.Join(s.Directory, entry.Name()))
		}
	}

	slices.Sort(files)
	return files, nil
}

// calculateTotalSize calculates the total size of all files.
func (s *FileBackingStore) calculateTotalSize(files []string) int64 {
	var totalSize int64
	for _, f := range files {
		totalSize += getFileSize(f)
	}
	return totalSize
}

// handleCorruptedEvent writes a corrupted event to the dead-letter file and increments error metrics.
func (s *FileBackingStore) handleCorruptedEvent(corruptedLine []byte, sourceFile string) {
	if err := s.writeDeadLetter(corruptedLine, sourceFile); err != nil {
		logg.Error("audittools: failed to write to dead-letter file: %s", err.Error())
		s.errorCounter.WithLabelValues("deadletter_write_failed").Inc()
	}
	s.errorCounter.WithLabelValues("corrupted_event").Inc()
}

// isEventFile returns true if the entry is a regular event file (not a directory or dead-letter file).
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
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
