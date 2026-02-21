package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu   sync.Mutex
	path string
}

type filePayload struct {
	Jobs []Job `json:"jobs"`
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		initial := filePayload{Jobs: []Job{}}
		data, _ := json.MarshalIndent(initial, "", "  ")
		if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
			return nil, writeErr
		}
	}
	return &Store{path: path}, nil
}

func (s *Store) List() ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	out := append([]Job{}, payload.Jobs...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) Upsert(job Job, now time.Time, defaultTZ string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(job.Kind.String()) == "" {
		job.Kind = KindUser
	}
	if strings.TrimSpace(job.Timezone) == "" {
		job.Timezone = defaultTZ
	}
	if err := job.Validate(); err != nil {
		return Job{}, err
	}

	payload, err := s.loadUnlocked()
	if err != nil {
		return Job{}, err
	}

	idx := -1
	for i := range payload.Jobs {
		if payload.Jobs[i].ID == job.ID {
			idx = i
			job.CreatedAt = payload.Jobs[i].CreatedAt
			break
		}
	}
	if job.CreatedAt == "" {
		job.CreatedAt = now.UTC().Format(time.RFC3339Nano)
	}
	job.UpdatedAt = now.UTC().Format(time.RFC3339Nano)

	next, err := computeNextRun(job, now)
	if err != nil {
		return Job{}, err
	}
	if !next.IsZero() {
		job.NextRunAt = next.UTC().Format(time.RFC3339Nano)
	} else {
		job.NextRunAt = ""
	}

	if idx >= 0 {
		payload.Jobs[idx] = job
	} else {
		payload.Jobs = append(payload.Jobs, job)
	}

	if err := s.saveUnlocked(payload); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := s.loadUnlocked()
	if err != nil {
		return false, err
	}
	out := make([]Job, 0, len(payload.Jobs))
	removed := false
	for _, j := range payload.Jobs {
		if j.ID == id {
			removed = true
			continue
		}
		out = append(out, j)
	}
	if !removed {
		return false, nil
	}
	payload.Jobs = out
	if err := s.saveUnlocked(payload); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) Due(now time.Time) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	due := []Job{}
	for _, j := range payload.Jobs {
		if !j.Enabled || j.NextRunAt == "" {
			continue
		}
		t, parseErr := time.Parse(time.RFC3339Nano, j.NextRunAt)
		if parseErr != nil {
			continue
		}
		if !t.After(now) {
			due = append(due, j)
		}
	}
	return due, nil
}

func (s *Store) MarkExecuted(id string, runAt time.Time, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	updated := false
	for i := range payload.Jobs {
		job := payload.Jobs[i]
		if job.ID != id {
			continue
		}
		job.LastRunAt = runAt.UTC().Format(time.RFC3339Nano)
		job.LastResult = result
		job.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		next, nextErr := computeNextRunAfter(job, runAt)
		if nextErr != nil {
			job.Enabled = false
			job.NextRunAt = ""
			job.LastResult = "error: " + nextErr.Error()
		} else {
			if next.IsZero() {
				job.Enabled = false
				job.NextRunAt = ""
			} else {
				job.NextRunAt = next.UTC().Format(time.RFC3339Nano)
			}
		}
		payload.Jobs[i] = job
		updated = true
		break
	}
	if !updated {
		return fmt.Errorf("job not found: %s", id)
	}
	return s.saveUnlocked(payload)
}

func (s *Store) loadUnlocked() (filePayload, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return filePayload{}, err
	}
	if len(data) == 0 {
		return filePayload{Jobs: []Job{}}, nil
	}
	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return filePayload{}, err
	}
	if payload.Jobs == nil {
		payload.Jobs = []Job{}
	}
	return payload, nil
}

func (s *Store) saveUnlocked(payload filePayload) error {
	if payload.Jobs == nil {
		payload.Jobs = []Job{}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func computeNextRun(job Job, from time.Time) (time.Time, error) {
	switch job.Mode {
	case ModeOnce:
		t, err := time.Parse(time.RFC3339, job.RunAt)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid runAt: %w", err)
		}
		return t, nil
	case ModeCron:
		loc, err := time.LoadLocation(job.Timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
		}
		next, err := nextCron(job.CronExpr, from, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
		}
		return next.UTC(), nil
	case ModeInterval:
		d, err := time.ParseDuration(job.Interval)
		if err != nil {
			return time.Time{}, err
		}
		return from.Add(d).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported mode: %s", job.Mode)
	}
}

func computeNextRunAfter(job Job, runAt time.Time) (time.Time, error) {
	switch job.Mode {
	case ModeOnce:
		return time.Time{}, nil
	case ModeCron:
		loc, err := time.LoadLocation(job.Timezone)
		if err != nil {
			return time.Time{}, err
		}
		next, err := nextCron(job.CronExpr, runAt, loc)
		if err != nil {
			return time.Time{}, err
		}
		return next.UTC(), nil
	case ModeInterval:
		d, err := time.ParseDuration(job.Interval)
		if err != nil {
			return time.Time{}, err
		}
		return runAt.Add(d).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported mode: %s", job.Mode)
	}
}

func (k JobKind) String() string {
	return string(k)
}
