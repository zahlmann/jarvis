package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/logstore"
)

type TriggerHandler func(ctx context.Context, trigger Trigger) error
type BusyFunc func(chatID int64) bool

type Engine struct {
	store     *Store
	heartbeat *Heartbeat
	handler   TriggerHandler
	busyFn    BusyFunc
	logger    *logstore.Store
}

func NewEngine(store *Store, heartbeat *Heartbeat, handler TriggerHandler, busyFn BusyFunc, logger *logstore.Store) *Engine {
	return &Engine{
		store:     store,
		heartbeat: heartbeat,
		handler:   handler,
		busyFn:    busyFn,
		logger:    logger,
	}
}

func (e *Engine) Start(ctx context.Context) {
	if e == nil {
		return
	}
	go e.run(ctx)
}

func (e *Engine) RunDue(ctx context.Context, now time.Time) error {
	due, err := e.store.Due(now)
	if err != nil {
		return err
	}
	for _, job := range due {
		trigger := Trigger{
			Kind:   job.Kind,
			JobID:  job.ID,
			ChatID: job.ChatID,
			Prompt: job.Prompt,
			Source: "schedule:" + job.ID,
		}
		runErr := e.handler(ctx, trigger)
		result := "ok"
		if runErr != nil {
			result = "error: " + runErr.Error()
		}
		if markErr := e.store.MarkExecuted(job.ID, now.UTC(), result); markErr != nil {
			_ = e.logger.Write("scheduler", "mark_executed_error", map[string]any{
				"job_id": job.ID,
				"error":  markErr.Error(),
			})
		}
		_ = e.logger.Write("scheduler", "job_triggered", map[string]any{
			"job_id":  job.ID,
			"chat_id": job.ChatID,
			"result":  result,
		})
	}
	return nil
}

func (e *Engine) run(ctx context.Context) {
	for {
		next := time.Now().UTC().Truncate(time.Minute).Add(time.Minute)
		wait := time.Until(next)
		if wait < 0 {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		now := time.Now().UTC()
		if err := e.RunDue(ctx, now); err != nil {
			_ = e.logger.Write("scheduler", "run_due_error", map[string]any{"error": err.Error()})
		}
		e.runHeartbeat(ctx, now)
	}
}

func (e *Engine) runHeartbeat(ctx context.Context, now time.Time) {
	if e.heartbeat == nil {
		return
	}
	busy := false
	if e.busyFn != nil {
		busy = e.busyFn(e.heartbeat.chatID)
	}
	trigger, decision, shouldRun, err := e.heartbeat.Tick(now, busy)
	if err != nil {
		_ = e.logger.Write("heartbeat", "tick_error", map[string]any{"error": err.Error()})
		return
	}
	_ = e.logger.Write("heartbeat", "decision", map[string]any{
		"decision": decision,
		"chat_id":  e.heartbeat.chatID,
		"busy":     busy,
	})
	if !shouldRun {
		return
	}
	if e.handler == nil {
		_ = e.logger.Write("heartbeat", "handler_missing", map[string]any{})
		return
	}
	if err := e.handler(ctx, trigger); err != nil {
		_ = e.logger.Write("heartbeat", "run_error", map[string]any{"error": err.Error()})
		return
	}
	_ = e.logger.Write("heartbeat", "run_ok", map[string]any{"chat_id": trigger.ChatID})
}

func (e *Engine) Require() error {
	if e.store == nil {
		return fmt.Errorf("scheduler store is required")
	}
	if e.handler == nil {
		return fmt.Errorf("scheduler handler is required")
	}
	return nil
}
