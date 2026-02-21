package logstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	mu      sync.Mutex
	baseDir string
}

func New(baseDir string) (*Store, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("baseDir is required")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{baseDir: baseDir}, nil
}

type Record map[string]any

func (s *Store) Write(component, event string, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	path := filepath.Join(s.baseDir, fmt.Sprintf("events-%s.jsonl", now.Format("2006-01-02")))

	payload := Record{
		"ts":        now.Format(time.RFC3339Nano),
		"component": component,
		"event":     event,
	}
	for k, v := range fields {
		payload[k] = v
	}

	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}
