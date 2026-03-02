package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/store"
	"github.com/zahlmann/phi"
	"github.com/zahlmann/phi/ai/model"
)

const (
	recentRecapExchanges = 10
	recentRecapTextLimit = 280
	finalEventTimeout    = 5 * time.Minute
)

type PromptInput struct {
	ChatID   int64
	UserName string
	Message  string
	Source   string
	ReplyTo  string
	IsVoice  bool
	Images   []model.ImageContent
	Metadata map[string]string
}

type Service struct {
	cfg    config.Config
	logger *logstore.Store

	runtime *phi.Runtime

	mu            sync.Mutex
	sessions      map[int64]*chatSession
	sessionToChat map[string]int64

	waitMu      sync.Mutex
	finalSeq    map[string]int64
	finalWaiter map[string][]chan struct{}

	recent *store.RecentStore
}

type chatSession struct {
	chatID int64

	mu      sync.Mutex
	running bool
	pending []PromptInput

	sessionID string
}

func New(cfg config.Config, logger *logstore.Store) *Service {
	recentStore, err := store.NewRecentStore(filepath.Join(cfg.DataDir, "messages", "recent"), store.DefaultRecentMaxMessages)
	if err != nil && logger != nil {
		_ = logger.Write("runtime", "recent_store_init_error", map[string]any{"error": err.Error()})
	}

	rt := phi.NewRuntime(phi.RuntimeOptions{
		AuthMode:     cfg.PhiAuthMode,
		APIKey:       cfg.PhiAPIKey,
		AccessToken:  cfg.PhiAccessToken,
		AccountID:    cfg.PhiAccountID,
		ModelID:      cfg.PhiModelID,
		SystemPrompt: cfg.PhiSystemPrompt,
		WorkingDir:   cfg.PhiToolRoot,
	})

	svc := &Service{
		cfg:           cfg,
		logger:        logger,
		runtime:       rt,
		sessions:      map[int64]*chatSession{},
		sessionToChat: map[string]int64{},
		finalSeq:      map[string]int64{},
		finalWaiter:   map[string][]chan struct{}{},
		recent:        recentStore,
	}

	rt.Subscribe(func(event phi.Event) {
		svc.handleRuntimeEvent(event)
	})

	return svc
}

func (s *Service) Close() {
	if s.runtime != nil {
		s.runtime.Close()
	}
}

func (s *Service) Enqueue(input PromptInput) {
	input.Message = strings.TrimSpace(input.Message)
	if input.Message == "" {
		return
	}
	if input.Source == "" {
		input.Source = "inbound"
	}

	cs := s.getOrCreateChatSession(input.ChatID)
	cs.mu.Lock()
	if cs.running {
		cs.pending = append(cs.pending, input)
		queued := len(cs.pending)
		cs.mu.Unlock()
		s.log("runtime", "queued_message", map[string]any{
			"chat_id": input.ChatID,
			"source":  input.Source,
			"queued":  queued,
		})
		return
	}
	cs.running = true
	cs.mu.Unlock()
	go s.runLoop(cs, input)
}

func (s *Service) IsBusy(chatID int64) bool {
	cs := s.getOrCreateChatSession(chatID)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.running
}

func (s *Service) runLoop(cs *chatSession, first PromptInput) {
	current := first
	for {
		if err := s.runPrompt(cs, current); err != nil {
			s.log("runtime", "prompt_error", map[string]any{
				"chat_id": cs.chatID,
				"source":  current.Source,
				"error":   err.Error(),
			})
		}

		cs.mu.Lock()
		if len(cs.pending) == 0 {
			cs.running = false
			cs.mu.Unlock()
			return
		}
		current = cs.pending[0]
		cs.pending = cs.pending[1:]
		remaining := len(cs.pending)
		cs.mu.Unlock()

		s.log("runtime", "dequeue_message", map[string]any{
			"chat_id":   cs.chatID,
			"source":    current.Source,
			"remaining": remaining,
		})
	}
}

func (s *Service) runPrompt(cs *chatSession, input PromptInput) error {
	s.log("runtime", "prompt_start", map[string]any{
		"chat_id": cs.chatID,
		"source":  input.Source,
		"voice":   input.IsVoice,
		"chars":   len(input.Message),
	})

	cs.mu.Lock()
	sessionID := strings.TrimSpace(cs.sessionID)
	cs.mu.Unlock()

	envelope := s.buildPromptEnvelope(input, sessionID == "")
	previousFinal := int64(0)
	if sessionID == "" {
		started, err := s.runtime.StartSession(context.Background(), phi.StartSessionRequest{
			Prompt: envelope,
			Images: input.Images,
		})
		if err != nil {
			return err
		}
		sessionID = started.SessionID
		cs.mu.Lock()
		if cs.sessionID == "" {
			cs.sessionID = sessionID
		}
		cs.mu.Unlock()
		s.mu.Lock()
		s.sessionToChat[sessionID] = cs.chatID
		s.mu.Unlock()
		s.log("runtime", "session_created", map[string]any{
			"chat_id":    cs.chatID,
			"session_id": sessionID,
			"model":      s.cfg.PhiModelID,
			"auth_mode":  string(s.cfg.PhiAuthMode),
		})
	} else {
		previousFinal = s.finalSequence(sessionID)
		if err := s.runtime.QueueMessage(context.Background(), phi.QueueMessageRequest{
			SessionID: sessionID,
			Prompt:    envelope,
			Images:    input.Images,
		}); err != nil {
			return err
		}
	}

	if err := s.waitForFinalEvent(sessionID, previousFinal, finalEventTimeout); err != nil {
		return err
	}

	s.log("runtime", "prompt_end", map[string]any{
		"chat_id":     cs.chatID,
		"source":      input.Source,
		"phi_session": sessionID,
	})
	return nil
}

func (s *Service) getOrCreateChatSession(chatID int64) *chatSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.sessions[chatID]; ok {
		return existing
	}
	created := &chatSession{chatID: chatID}
	s.sessions[chatID] = created
	return created
}

func (s *Service) handleRuntimeEvent(event phi.Event) {
	s.logPhiEvent(event)

	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return
	}

	if event.Type == phi.EventFinalMessage {
		s.markFinalEvent(sessionID)
	}
}

func (s *Service) markFinalEvent(sessionID string) {
	s.waitMu.Lock()
	s.finalSeq[sessionID] = s.finalSeq[sessionID] + 1
	waiters := s.finalWaiter[sessionID]
	delete(s.finalWaiter, sessionID)
	s.waitMu.Unlock()

	for _, waiter := range waiters {
		if waiter != nil {
			close(waiter)
		}
	}
}

func (s *Service) finalSequence(sessionID string) int64 {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	return s.finalSeq[sessionID]
}

func (s *Service) waitForFinalEvent(sessionID string, previous int64, timeout time.Duration) error {
	s.waitMu.Lock()
	if s.finalSeq[sessionID] > previous {
		s.waitMu.Unlock()
		return nil
	}
	waiter := make(chan struct{})
	s.finalWaiter[sessionID] = append(s.finalWaiter[sessionID], waiter)
	s.waitMu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-waiter:
		return nil
	case <-timer.C:
		s.waitMu.Lock()
		if s.finalSeq[sessionID] > previous {
			s.removeFinalWaiterLocked(sessionID, waiter)
			s.waitMu.Unlock()
			return nil
		}
		s.removeFinalWaiterLocked(sessionID, waiter)
		s.waitMu.Unlock()
		return fmt.Errorf("timed out waiting for final message in session %s", sessionID)
	}
}

func (s *Service) removeFinalWaiterLocked(sessionID string, waiter chan struct{}) {
	waiters := s.finalWaiter[sessionID]
	if len(waiters) == 0 {
		return
	}
	filtered := make([]chan struct{}, 0, len(waiters))
	for _, ch := range waiters {
		if ch != waiter {
			filtered = append(filtered, ch)
		}
	}
	if len(filtered) == 0 {
		delete(s.finalWaiter, sessionID)
		return
	}
	s.finalWaiter[sessionID] = filtered
}

func (s *Service) buildPromptEnvelope(input PromptInput, includeRecentRecap bool) string {
	loc := time.UTC
	if tz, err := time.LoadLocation(s.cfg.Timezone); err == nil {
		loc = tz
	}
	now := time.Now().In(loc)

	parts := []string{
		"[Platform: Telegram]",
		fmt.Sprintf("[Chat ID: %d]", input.ChatID),
		fmt.Sprintf("[User: %s]", strings.TrimSpace(input.UserName)),
		fmt.Sprintf("[Source: %s]", input.Source),
		fmt.Sprintf("[Local time: %s]", now.Format("2006-01-02 15:04 MST")),
		fmt.Sprintf("[Repo root: %s]", s.cfg.PhiToolRoot),
		fmt.Sprintf("[Voice transcription enabled: %t]", s.cfg.TranscriptionEnabled),
		fmt.Sprintf("[Voice reply enabled: %t]", s.cfg.VoiceReplyEnabled),
	}
	if input.IsVoice {
		parts = append(parts, "[Voice message transcription]")
	}
	if strings.TrimSpace(input.ReplyTo) != "" {
		parts = append(parts, fmt.Sprintf("[Replying to: %s]", input.ReplyTo))
	}
	if includeRecentRecap {
		if recap := s.buildRecentRecap(input, recentRecapExchanges); recap != "" {
			parts = append(parts, "")
			parts = append(parts, recap)
		}
	}
	parts = append(parts, "")
	parts = append(parts, "Message: "+input.Message)
	return strings.Join(parts, "\n")
}

func (s *Service) buildRecentRecap(input PromptInput, limit int) string {
	if s.recent == nil || input.ChatID == 0 || limit <= 0 {
		return ""
	}

	exchanges, err := s.recent.LastExchanges(input.ChatID, limit+1)
	if err != nil || len(exchanges) == 0 {
		return ""
	}

	currentMessage := strings.TrimSpace(input.Message)
	if currentMessage != "" {
		last := exchanges[len(exchanges)-1]
		if strings.TrimSpace(last.User.Text) == currentMessage && len(last.Jarvis) == 0 {
			exchanges = exchanges[:len(exchanges)-1]
		}
	}
	if len(exchanges) == 0 {
		return ""
	}
	if len(exchanges) > limit {
		exchanges = exchanges[len(exchanges)-limit:]
	}

	lines := []string{
		fmt.Sprintf("[Recent recap: %d prior user/jarvis exchange(s) from persistent history; use only when relevant.]", len(exchanges)),
	}
	for idx, exchange := range exchanges {
		n := idx + 1
		userText := truncatePromptText(exchange.User.Text, recentRecapTextLimit)
		if userText == "" {
			userText = "(empty)"
		}
		lines = append(lines, fmt.Sprintf("recent %d user: %s", n, userText))

		if len(exchange.Jarvis) == 0 {
			lines = append(lines, fmt.Sprintf("recent %d jarvis: (no reply recorded)", n))
			continue
		}

		replies := make([]string, 0, len(exchange.Jarvis))
		for _, reply := range exchange.Jarvis {
			text := truncatePromptText(reply.Text, recentRecapTextLimit)
			if text != "" {
				replies = append(replies, text)
			}
		}
		if len(replies) == 0 {
			lines = append(lines, fmt.Sprintf("recent %d jarvis: (empty)", n))
			continue
		}
		lines = append(lines, fmt.Sprintf("recent %d jarvis: %s", n, strings.Join(replies, " | ")))
	}

	return strings.Join(lines, "\n")
}

func truncatePromptText(raw string, maxChars int) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if maxChars <= 0 {
		return normalized
	}
	runes := []rune(normalized)
	if len(runes) <= maxChars {
		return normalized
	}
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-3]) + "..."
}

func (s *Service) logPhiEvent(event phi.Event) {
	fields := map[string]any{
		"event_type": string(event.Type),
		"session_id": event.SessionID,
	}
	if chatID, ok := s.chatIDForSession(event.SessionID); ok {
		fields["chat_id"] = chatID
	}
	if event.ToolName != "" {
		fields["tool_name"] = event.ToolName
	}
	if event.ToolCallID != "" {
		fields["tool_call_id"] = event.ToolCallID
	}
	if event.IsError {
		fields["is_error"] = true
	}
	if event.ToolResult != nil {
		if txt := extractText(event.ToolResult.ContentRaw); strings.TrimSpace(txt) != "" {
			fields["tool_result"] = txt
		}
	}
	if event.AssistantMessage != nil {
		if txt := extractText(event.AssistantMessage.ContentRaw); strings.TrimSpace(txt) != "" {
			fields["assistant_text"] = txt
		}
		if event.AssistantMessage.StopReason != "" {
			fields["stop_reason"] = string(event.AssistantMessage.StopReason)
		}
	}
	if strings.TrimSpace(event.Error) != "" {
		fields["error"] = event.Error
	}
	s.log("phi", "runtime_event", fields)
}

func (s *Service) chatIDForSession(sessionID string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.sessionToChat[sessionID]
	return id, ok
}

func extractText(content []any) string {
	parts := []string{}
	for _, item := range content {
		switch v := item.(type) {
		case model.TextContent:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, v.Text)
			}
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == string(model.ContentText) {
				text, _ := v["text"].(string)
				if strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (s *Service) log(component, action string, fields map[string]any) {
	if s.logger == nil {
		return
	}
	_ = s.logger.Write(component, action, fields)
}
