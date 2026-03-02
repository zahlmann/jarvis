package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/logstore"
)

type TriggerHandler func(ctx context.Context, trigger Trigger) error

type Engine struct {
	store   *Store
	handler TriggerHandler
	logger  *logstore.Store
}

func NewEngine(store *Store, handler TriggerHandler, logger *logstore.Store) *Engine {
	return &Engine{
		store:   store,
		handler: handler,
		logger:  logger,
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
	}
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
