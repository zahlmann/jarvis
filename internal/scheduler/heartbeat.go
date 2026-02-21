package scheduler

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

type Heartbeat struct {
	path    string
	enabled bool
	chatID  int64
	prompt  string
	rand    *rand.Rand
}

type HeartbeatState struct {
	CycleBase string `json:"cycleBase,omitempty"`
	DueAt     string `json:"dueAt,omitempty"`
	WindowEnd string `json:"windowEnd,omitempty"`
	OffsetMin int    `json:"offsetMin,omitempty"`
	Status    string `json:"status,omitempty"`
}

func NewHeartbeat(path string, enabled bool, chatID int64, prompt string) (*Heartbeat, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &Heartbeat{
		path:    path,
		enabled: enabled,
		chatID:  chatID,
		prompt:  prompt,
		rand:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func (h *Heartbeat) Tick(now time.Time, busy bool) (Trigger, string, bool, error) {
	if !h.enabled || h.chatID == 0 {
		return Trigger{}, "disabled", false, nil
	}
	state, err := h.load()
	if err != nil {
		return Trigger{}, "load_error", false, err
	}

	cycleBase := floor30(now.UTC())
	if state.CycleBase == "" || state.CycleBase != cycleBase.Format(time.RFC3339Nano) {
		offset := h.rand.Intn(21) - 10
		due := cycleBase.Add(time.Duration(offset) * time.Minute)
		windowEnd := cycleBase.Add(10 * time.Minute)
		state = HeartbeatState{
			CycleBase: cycleBase.Format(time.RFC3339Nano),
			DueAt:     due.Format(time.RFC3339Nano),
			WindowEnd: windowEnd.Format(time.RFC3339Nano),
			OffsetMin: offset,
			Status:    "scheduled",
		}
		if err := h.save(state); err != nil {
			return Trigger{}, "save_error", false, err
		}
	}

	if state.Status == "executed" || state.Status == "skipped" {
		return Trigger{}, "already_handled", false, nil
	}

	dueAt, err := time.Parse(time.RFC3339Nano, state.DueAt)
	if err != nil {
		return Trigger{}, "parse_error", false, err
	}
	windowEnd, err := time.Parse(time.RFC3339Nano, state.WindowEnd)
	if err != nil {
		return Trigger{}, "parse_error", false, err
	}

	if now.Before(dueAt) {
		return Trigger{}, "waiting", false, nil
	}
	if now.After(windowEnd) {
		state.Status = "skipped"
		if err := h.save(state); err != nil {
			return Trigger{}, "save_error", false, err
		}
		return Trigger{}, "skipped_busy_or_missed", false, nil
	}
	if busy {
		return Trigger{}, "delayed_busy", false, nil
	}

	state.Status = "executed"
	if err := h.save(state); err != nil {
		return Trigger{}, "save_error", false, err
	}

	return Trigger{
		Kind:   KindHeartbeat,
		JobID:  "heartbeat",
		ChatID: h.chatID,
		Prompt: h.prompt,
		Source: "heartbeat",
	}, "executed", true, nil
}

func (h *Heartbeat) load() (HeartbeatState, error) {
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return HeartbeatState{}, nil
		}
		return HeartbeatState{}, err
	}
	if len(data) == 0 {
		return HeartbeatState{}, nil
	}
	var state HeartbeatState
	if err := json.Unmarshal(data, &state); err != nil {
		return HeartbeatState{}, err
	}
	return state, nil
}

func (h *Heartbeat) save(state HeartbeatState) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, payload, 0o644)
}

func floor30(t time.Time) time.Time {
	minutes := (t.Minute() / 30) * 30
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minutes, 0, 0, time.UTC)
}
