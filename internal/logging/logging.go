// Package logging writes the agent log to a rotating file and to stderr.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxLogBytes = 5 << 20
	maxLogFiles = 3
)

// New opens <dir>/agent.log, rotating it at 5 MB and keeping three files.
func New(dir, level string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("cannot create the log directory %s: %w", dir, err)
	}

	writer, err := newRotatingWriter(filepath.Join(dir, "agent.log"), maxLogBytes, maxLogFiles)
	if err != nil {
		return nil, nil, err
	}

	handler := slog.NewTextHandler(io.MultiWriter(writer, os.Stderr), &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler), writer, nil
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

func newRotatingWriter(path string, maxBytes int64, maxFiles int) (*rotatingWriter, error) {
	writer := &rotatingWriter{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", w.path, err)
	}
	info, statErr := file.Stat()
	if statErr == nil {
		w.size = info.Size()
	}
	w.file = file
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > w.maxBytes {
		w.rotate()
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	for index := w.maxFiles; index >= 1; index-- {
		older := fmt.Sprintf("%s.%d", w.path, index)
		newer := fmt.Sprintf("%s.%d", w.path, index+1)
		_ = os.Rename(older, newer)
	}
	_ = os.Rename(w.path, w.path+".1")
	w.size = 0
	_ = w.open()
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
