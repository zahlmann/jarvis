package scheduler

import (
	"fmt"
	"time"
)

type JobMode string

type JobKind string

const (
	ModeOnce     JobMode = "once"
	ModeCron     JobMode = "cron"
	ModeInterval JobMode = "interval"

	KindUser      JobKind = "user"
	KindHeartbeat JobKind = "heartbeat"
)

type Job struct {
	ID         string  `json:"id"`
	Kind       JobKind `json:"kind"`
	ChatID     int64   `json:"chatId"`
	Prompt     string  `json:"prompt"`
	Mode       JobMode `json:"mode"`
	CronExpr   string  `json:"cronExpr,omitempty"`
	RunAt      string  `json:"runAt,omitempty"`
	Interval   string  `json:"interval,omitempty"`
	Timezone   string  `json:"timezone,omitempty"`
	Enabled    bool    `json:"enabled"`
	NextRunAt  string  `json:"nextRunAt,omitempty"`
	LastRunAt  string  `json:"lastRunAt,omitempty"`
	LastResult string  `json:"lastResult,omitempty"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

func (j Job) Validate() error {
	if j.ID == "" {
		return fmt.Errorf("job id is required")
	}
	if j.ChatID == 0 {
		return fmt.Errorf("chat id is required")
	}
	if j.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if j.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	switch j.Mode {
	case ModeOnce:
		if j.RunAt == "" {
			return fmt.Errorf("runAt is required for once jobs")
		}
	case ModeCron:
		if j.CronExpr == "" {
			return fmt.Errorf("cronExpr is required for cron jobs")
		}
	case ModeInterval:
		if j.Interval == "" {
			return fmt.Errorf("interval is required for interval jobs")
		}
		if _, err := time.ParseDuration(j.Interval); err != nil {
			return fmt.Errorf("invalid interval: %w", err)
		}
	default:
		return fmt.Errorf("unsupported mode: %s", j.Mode)
	}
	return nil
}

type Trigger struct {
	Kind   JobKind
	JobID  string
	ChatID int64
	Prompt string
	Source string
}
