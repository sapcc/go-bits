// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/option"

	"github.com/sapcc/go-bits/logg"
)

// NewFileBackingStore is a BackingStoreFactory for a BackingStore that keeps events in files in a local directory.
// To keep events across restarts, the directory must be on a persistent volume.
// The directory must not be shared with other processes. Params:
//
//	{
//	  "directory": "/var/cache/audit",  // required
//	  "max_file_size": 10485760,        // optional; once a file is this large, a new file is started (default: 10 MiB)
//	  "max_total_size": 1073741824      // optional; default: 0 (unlimited)
//	}
//
// Each file is read and published as one batch.
// If max_total_size is set and reached, Auditor.Record() blocks until RabbitMQ is reachable again.
// Lines that cannot be decoded are moved into a file with the suffix ".corrupt" for manual inspection.
func NewFileBackingStore(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
	var cfg struct {
		Directory    string               `json:"directory"`
		MaxFileSize  option.Option[int64] `json:"max_file_size"`
		MaxTotalSize option.Option[int64] `json:"max_total_size"`
	}
	err := unmarshalJSONStrict(params, &cfg)
	if err != nil {
		return nil, fmt.Errorf("audittools: cannot parse params for file backing store: %w", err)
	}
	if cfg.Directory == "" {
		return nil, errors.New("audittools: missing required param for file backing store: directory")
	}

	s := &fileBackingStore{
		directory:    cfg.Directory,
		maxFileSize:  cfg.MaxFileSize.UnwrapOr(10 << 20),
		maxTotalSize: cfg.MaxTotalSize.UnwrapOr(0),
		sizeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "audittools_backing_store_size_bytes",
			Help: "Total size of the files in the backing store.",
		}),
		filesGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "audittools_backing_store_files",
			Help: "Number of files in the backing store.",
		}),
	}
	if s.maxFileSize <= 0 {
		return nil, fmt.Errorf("audittools: invalid max_file_size for file backing store: %d", s.maxFileSize)
	}
	if s.maxTotalSize < 0 {
		return nil, fmt.Errorf("audittools: invalid max_total_size for file backing store: %d", s.maxTotalSize)
	}

	// audit events can contain sensitive data
	err = os.MkdirAll(s.directory, 0700)
	if err != nil {
		return nil, fmt.Errorf("audittools: cannot create directory for file backing store: %w", err)
	}
	err = os.Chmod(s.directory, 0700) // in case it already existed
	if err != nil {
		return nil, fmt.Errorf("audittools: cannot chmod directory for file backing store: %w", err)
	}

	err = s.UpdateMetrics()
	if err != nil {
		return nil, err
	}
	s.metrics = newBackingStoreMetrics(opts.Registry, s.sizeGauge, s.filesGauge)
	return s, nil
}

// fileBackingStore is the BackingStore built by NewFileBackingStore.
// Events are stored as one JSON document per line.
type fileBackingStore struct {
	directory    string
	maxFileSize  int64
	maxTotalSize int64

	currentFile     string // where Write() appends to (empty if a new file shall be started)
	currentFileSize int64
	totalSize       int64

	metrics    backingStoreMetrics
	sizeGauge  prometheus.Gauge
	filesGauge prometheus.Gauge
}

// Write implements the BackingStore interface.
func (s *fileBackingStore) Write(event cadf.Event) error {
	if s.maxTotalSize > 0 && s.totalSize >= s.maxTotalSize {
		s.metrics.Errors.WithLabelValues("write").Inc()
		return fmt.Errorf("%w (max_total_size = %d)", errBackingStoreFull, s.maxTotalSize)
	}

	buf, err := json.Marshal(event)
	if err != nil {
		s.metrics.Errors.WithLabelValues("write").Inc()
		return fmt.Errorf("audittools: cannot serialize event for file backing store: %w", err)
	}
	buf = append(buf, '\n')

	isNewFile := s.currentFile == "" || s.currentFileSize >= s.maxFileSize
	if isNewFile {
		s.currentFile = filepath.Join(s.directory, fmt.Sprintf("audit-events-%d.jsonl", time.Now().UnixNano()))
		s.currentFileSize = 0
	}
	err = appendToFile(s.currentFile, buf)
	if err == nil && isNewFile {
		err = syncDir(s.directory)
	}
	if err != nil {
		// the file may now end in a partial line, so do not append further events to it
		s.currentFile = ""
		s.metrics.Errors.WithLabelValues("write").Inc()
		return fmt.Errorf("audittools: cannot write to file backing store: %w", err)
	}

	s.currentFileSize += int64(len(buf))
	s.totalSize += int64(len(buf))
	s.metrics.Writes.Inc()
	return nil
}

// ReadBatch implements the BackingStore interface.
func (s *fileBackingStore) ReadBatch() ([]cadf.Event, func(int) error, error) {
	files, _, err := s.listFiles()
	if err != nil {
		s.metrics.Errors.WithLabelValues("read").Inc()
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, nil
	}
	path := files[0]
	if path == s.currentFile {
		// commit will remove or replace this file, so do not append to it anymore
		s.currentFile = ""
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		s.metrics.Errors.WithLabelValues("read").Inc()
		return nil, nil, fmt.Errorf("audittools: cannot read from file backing store: %w", err)
	}
	var (
		events       []cadf.Event
		eventLines   [][]byte // the lines from which `events` were decoded, in the same order
		corruptLines [][]byte
	)
	for line := range bytes.Lines(contents) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event cadf.Event
		if json.Unmarshal(line, &event) != nil {
			corruptLines = append(corruptLines, line)
			continue
		}
		events = append(events, event)
		eventLines = append(eventLines, line)
	}

	commit := func(published int) error {
		err := s.commit(path, published, eventLines, corruptLines)
		if err != nil {
			s.metrics.Errors.WithLabelValues("commit").Inc()
			return err
		}
		if published > 0 || len(events) == 0 {
			s.totalSize -= int64(len(contents))
			if published < len(events) {
				s.totalSize += int64(len(joinLines(eventLines[published:], corruptLines)))
			}
		}
		s.metrics.Reads.Add(float64(published))
		return nil
	}
	return events, commit, nil
}

func (s *fileBackingStore) commit(path string, published int, eventLines, corruptLines [][]byte) error {
	switch {
	case published == 0 && len(eventLines) > 0:
		return nil
	case published < len(eventLines):
		// keep what was not published yet for the next ReadBatch
		return writeFileAtomically(path, joinLines(eventLines[published:], corruptLines))
	case len(corruptLines) > 0:
		logg.Error("audittools: moving %d lines that cannot be decoded as audit events into %s.corrupt", len(corruptLines), path)
		s.metrics.Errors.WithLabelValues("read").Add(float64(len(corruptLines)))
		err := writeFileAtomically(path+".corrupt", joinLines(corruptLines))
		if err != nil {
			return err
		}
		return os.Remove(path)
	default:
		return os.Remove(path)
	}
}

// UpdateMetrics implements the BackingStore interface.
func (s *fileBackingStore) UpdateMetrics() error {
	files, totalSize, err := s.listFiles()
	if err != nil {
		return err
	}
	s.totalSize = totalSize // in case our own bookkeeping has drifted
	s.sizeGauge.Set(float64(totalSize))
	s.filesGauge.Set(float64(len(files)))
	return nil
}

// listFiles returns the event files in the order in which they were written, as well as their total size.
func (s *fileBackingStore) listFiles() (files []string, totalSize int64, err error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return nil, 0, fmt.Errorf("audittools: cannot list files in file backing store: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "audit-events-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(s.directory, name))
		info, err := entry.Info()
		if err == nil {
			totalSize += info.Size()
		}
	}
	slices.Sort(files) // file names contain a timestamp
	return files, totalSize, nil
}

// joinLines concatenates the given lines with a newline after each one.
func joinLines(lineSets ...[][]byte) []byte {
	var buf []byte
	for _, lines := range lineSets {
		for _, line := range lines {
			buf = append(buf, line...)
			buf = append(buf, '\n')
		}
	}
	return buf
}

func appendToFile(path string, buf []byte) (err error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}()

	_, err = f.Write(buf)
	if err != nil {
		return err
	}
	// fsync so that the event survives a node crash, not just a process crash
	// (for a new file, Write also fsyncs the directory)
	return f.Sync()
}

func writeFileAtomically(path string, buf []byte) (err error) {
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmpPath, path)
		}
		if err != nil {
			_ = os.Remove(tmpPath)
			return
		}
		err = syncDir(filepath.Dir(path))
	}()

	_, err = f.Write(buf)
	if err != nil {
		return err
	}
	return f.Sync()
}

// syncDir fsyncs a directory, so that files created in it or renamed into it survive a node crash.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
