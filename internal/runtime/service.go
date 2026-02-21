package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
	"github.com/zahlmann/phi/coding/sdk"
	"github.com/zahlmann/phi/coding/session"
	"github.com/zahlmann/phi/coding/tools"
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

	provider provider.Client

	mu       sync.Mutex
	sessions map[int64]*chatSession

	trackMu    sync.Mutex
	sendCalled map[int64]bool
}

type chatSession struct {
	chatID int64

	mu      sync.Mutex
	running bool
	pending []PromptInput

	session     *sdk.AgentSession
	unsubscribe func()
}

func New(cfg config.Config, logger *logstore.Store) *Service {
	return &Service{
		cfg:        cfg,
		logger:     logger,
		provider:   provider.NewOpenAIClient(),
		sessions:   map[int64]*chatSession{},
		sendCalled: map[int64]bool{},
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
		_ = s.logger.Write("runtime", "queued_message", map[string]any{
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
			_ = s.logger.Write("runtime", "prompt_error", map[string]any{
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

		_ = s.logger.Write("runtime", "dequeue_message", map[string]any{
			"chat_id":   cs.chatID,
			"source":    current.Source,
			"remaining": remaining,
		})
	}
}

func (s *Service) runPrompt(cs *chatSession, input PromptInput) error {
	agentSession, err := s.ensureSession(cs)
	if err != nil {
		return err
	}

	envelope := s.buildPromptEnvelope(input)
	_ = s.logger.Write("runtime", "prompt_start", map[string]any{
		"chat_id": cs.chatID,
		"source":  input.Source,
		"voice":   input.IsVoice,
		"chars":   len(input.Message),
	})

	s.setSendCalled(cs.chatID, false)
	err = agentSession.Prompt(envelope, sdk.PromptOptions{Images: input.Images})
	if err != nil {
		return err
	}
	if !s.getSendCalled(cs.chatID) {
		_ = s.logger.Write("runtime", "no_explicit_send", map[string]any{
			"chat_id": cs.chatID,
			"source":  input.Source,
		})
	}

	_ = s.logger.Write("runtime", "prompt_end", map[string]any{
		"chat_id": cs.chatID,
		"source":  input.Source,
	})
	return nil
}

func (s *Service) ensureSession(cs *chatSession) (*sdk.AgentSession, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.session != nil {
		return cs.session, nil
	}

	sessionID := fmt.Sprintf("chat-%d", cs.chatID)
	sessionPath := filepath.Join(s.cfg.DataDir, "sessions", sessionID+".jsonl")
	var mgr session.Manager
	fileMgr, err := session.NewFileManager(sessionID, sessionPath)
	if err != nil {
		mgr = session.NewInMemoryManager(sessionID)
	} else {
		mgr = fileMgr
	}

	newSession := sdk.CreateAgentSession(sdk.CreateSessionOptions{
		SystemPrompt:   s.cfg.PhiSystemPrompt,
		Model:          &model.Model{Provider: "openai", ID: s.cfg.PhiModelID},
		ThinkingLevel:  s.cfg.PhiThinking,
		Tools:          tools.NewCodingTools(s.cfg.PhiToolRoot),
		SessionManager: mgr,
		ProviderClient: s.provider,
		AuthMode:       s.cfg.PhiAuthMode,
		APIKey:         s.cfg.PhiAPIKey,
		AccessToken:    s.cfg.PhiAccessToken,
		AccountID:      s.cfg.PhiAccountID,
	})

	unsubscribe := newSession.Subscribe(func(ev agent.Event) {
		s.logAgentEvent(cs.chatID, ev)
	})

	cs.session = newSession
	cs.unsubscribe = unsubscribe
	_ = s.logger.Write("runtime", "session_created", map[string]any{
		"chat_id":    cs.chatID,
		"session_id": sessionID,
		"model":      s.cfg.PhiModelID,
		"auth_mode":  string(s.cfg.PhiAuthMode),
	})

	return cs.session, nil
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

func (s *Service) buildPromptEnvelope(input PromptInput) string {
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
		fmt.Sprintf("[Voice transcription enabled: %t]", s.cfg.TranscriptionEnabled),
		fmt.Sprintf("[Voice reply enabled: %t]", s.cfg.VoiceReplyEnabled),
	}
	if input.IsVoice {
		parts = append(parts, "[Voice message transcription]")
	}
	if strings.TrimSpace(input.ReplyTo) != "" {
		parts = append(parts, fmt.Sprintf("[Replying to: %s]", input.ReplyTo))
	}
	parts = append(parts, "")
	parts = append(parts, "Message: "+input.Message)
	return strings.Join(parts, "\n")
}

func (s *Service) logAgentEvent(chatID int64, ev agent.Event) {
	fields := map[string]any{
		"chat_id":    chatID,
		"event_type": string(ev.Type),
	}
	if ev.ToolName != "" {
		fields["tool_name"] = ev.ToolName
	}
	if ev.ToolCallID != "" {
		fields["tool_call_id"] = ev.ToolCallID
	}
	if ev.IsError {
		fields["is_error"] = true
	}

	if se, ok := ev.Message.(stream.Event); ok {
		fields["stream_type"] = string(se.Type)
		if se.Delta != "" {
			fields["delta"] = se.Delta
		}
		if se.Reason != "" {
			fields["reason"] = string(se.Reason)
		}
		if se.Error != "" {
			fields["error"] = se.Error
		}
		if se.ToolName != "" && fields["tool_name"] == nil {
			fields["tool_name"] = se.ToolName
		}
		if se.ToolCallID != "" && fields["tool_call_id"] == nil {
			fields["tool_call_id"] = se.ToolCallID
		}
		if se.Type == stream.EventToolCall && strings.EqualFold(se.ToolName, "bash") {
			cmd, _ := se.Arguments["command"].(string)
			if looksLikeTelegramSend(cmd) {
				s.setSendCalled(chatID, true)
			}
		}
	}

	switch msg := ev.Message.(type) {
	case model.AssistantMessage:
		text := extractText(msg.ContentRaw)
		if strings.TrimSpace(text) != "" {
			fields["assistant_text"] = text
		}
		fields["stop_reason"] = string(msg.StopReason)
	case model.Message:
		if msg.Role == model.RoleToolResult {
			fields["tool_result"] = extractText(msg.ContentRaw)
		}
	}

	_ = s.logger.Write("phi", "agent_event", fields)
}

func looksLikeTelegramSend(cmd string) bool {
	normalized := strings.ToLower(strings.TrimSpace(cmd))
	return strings.Contains(normalized, "jarvisctl telegram send") ||
		strings.Contains(normalized, "go run ./cmd/jarvisctl telegram send") ||
		strings.Contains(normalized, "go run ./cmd/jarvisctl -- telegram send")
}

func (s *Service) setSendCalled(chatID int64, value bool) {
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	s.sendCalled[chatID] = value
}

func (s *Service) getSendCalled(chatID int64) bool {
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	return s.sendCalled[chatID]
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
