package scheduler

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHeartbeatExecutesWhenDueAndIdle(t *testing.T) {
	tmp := t.TempDir()
	h, err := NewHeartbeat(filepath.Join(tmp, "heartbeat.json"), true, 123, "hb prompt")
	if err != nil {
		t.Fatalf("NewHeartbeat failed: %v", err)
	}

	now := time.Date(2026, 2, 21, 12, 5, 0, 0, time.UTC)
	state := HeartbeatState{
		CycleBase: floor30(now).Format(time.RFC3339Nano),
		DueAt:     now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
		WindowEnd: now.Add(5 * time.Minute).Format(time.RFC3339Nano),
		Status:    "scheduled",
	}
	if err := h.save(state); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	trigger, decision, shouldRun, err := h.Tick(now, false)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if !shouldRun {
		t.Fatalf("expected heartbeat to run, decision=%s", decision)
	}
	if trigger.ChatID != 123 || trigger.Kind != KindHeartbeat {
		t.Fatalf("unexpected trigger: %#v", trigger)
	}
}

func TestHeartbeatSkipsAfterWindow(t *testing.T) {
	tmp := t.TempDir()
	h, err := NewHeartbeat(filepath.Join(tmp, "heartbeat.json"), true, 123, "hb prompt")
	if err != nil {
		t.Fatalf("NewHeartbeat failed: %v", err)
	}

	now := time.Date(2026, 2, 21, 12, 20, 0, 0, time.UTC)
	state := HeartbeatState{
		CycleBase: floor30(now).Format(time.RFC3339Nano),
		DueAt:     now.Add(-20 * time.Minute).Format(time.RFC3339Nano),
		WindowEnd: now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
		Status:    "scheduled",
	}
	if err := h.save(state); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	_, decision, shouldRun, err := h.Tick(now, true)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}
	if shouldRun {
		t.Fatalf("expected heartbeat not to run, decision=%s", decision)
	}
	if decision != "skipped_busy_or_missed" {
		t.Fatalf("unexpected decision: %s", decision)
	}
}
