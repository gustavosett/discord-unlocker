package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxLogBytes = 1 << 20
	logBackups  = 3
)

// Logger writes a small rotating diagnostic log. Manual runs are mirrored to
// the console; autostart runs only write the file.
type Logger struct {
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
}

func Open(path string, console io.Writer) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("criar pasta de log: %w", err)
	}
	if err := rotateIfNeeded(path); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("abrir log: %w", err)
	}

	var out io.Writer = f
	if console != nil {
		out = io.MultiWriter(console, f)
	}
	return &Logger{logger: log.New(out, "", log.Ldate|log.Ltime|log.Lmicroseconds), file: f}, nil
}

func (l *Logger) Printf(format string, args ...any) {
	if l == nil || l.logger == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Printf(format, args...)
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

func rotateIfNeeded(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("examinar log: %w", err)
	}
	if info.Size() < maxLogBytes {
		return nil
	}

	for i := logBackups; i >= 1; i-- {
		dst := fmt.Sprintf("%s.%d", path, i)
		_ = os.Remove(dst)
		if i == 1 {
			if err := os.Rename(path, dst); err != nil {
				return fmt.Errorf("rotacionar log: %w", err)
			}
			continue
		}
		src := fmt.Sprintf("%s.%d", path, i-1)
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotacionar log antigo: %w", err)
		}
	}
	return nil
}
