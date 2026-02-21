package scheduler

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreOnceJobLifecycle(t *testing.T) {
	tmp := t.TempDir()
	st, err := NewStore(filepath.Join(tmp, "jobs.json"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	runAt := now.Add(2 * time.Minute).Format(time.RFC3339)
	job, err := st.Upsert(Job{
		ID:      "once-1",
		Kind:    KindUser,
		ChatID:  99,
		Prompt:  "ping",
		Mode:    ModeOnce,
		RunAt:   runAt,
		Enabled: true,
	}, now, "UTC")
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	if job.NextRunAt == "" {
		t.Fatalf("expected NextRunAt")
	}

	due, err := st.Due(now.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("Due failed: %v", err)
	}
	if len(due) != 1 || due[0].ID != "once-1" {
		t.Fatalf("unexpected due jobs: %#v", due)
	}

	if err := st.MarkExecuted("once-1", now.Add(3*time.Minute), "ok"); err != nil {
		t.Fatalf("MarkExecuted failed: %v", err)
	}
	jobs, err := st.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Enabled {
		t.Fatalf("once job should be disabled after execution")
	}
}
