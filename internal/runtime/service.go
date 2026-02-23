package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/store"
	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
	"github.com/zahlmann/phi/coding/sdk"
	"github.com/zahlmann/phi/coding/session"
	"github.com/zahlmann/phi/coding/tools"
)

var okTruePattern = regexp.MustCompile(`"ok"\s*:\s*true`)

const (
	sessionIdleTimeout   = 30 * time.Minute
	recentRecapExchanges = 10
	recentRecapTextLimit = 280
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

	trackMu  sync.Mutex
	attempts map[int64]*attemptTracking

	recent *store.RecentStore
}

type callKind uint8

const (
	callKindUnknown callKind = iota
	callKindSend
	callKindWork
)

type attemptTracking struct {
	pendingCalls map[string]callKind
	sequence     int
	lastSendSeq  int
	lastWorkSeq  int
	sendCalled   bool
}

type attemptStatus struct {
	sendCalled    bool
	sendAfterWork bool
}

type chatSession struct {
	chatID int64

	mu              sync.Mutex
	running         bool
	pending         []PromptInput
	lastInteraction time.Time

	session     *sdk.AgentSession
	unsubscribe func()
}

func New(cfg config.Config, logger *logstore.Store) *Service {
	recentStore, err := store.NewRecentStore(filepath.Join(cfg.DataDir, "messages", "recent"), store.DefaultRecentMaxMessages)
	if err != nil && logger != nil {
		_ = logger.Write("runtime", "recent_store_init_error", map[string]any{"error": err.Error()})
	}

	return &Service{
		cfg:      cfg,
		logger:   logger,
		provider: provider.NewOpenAIClient(),
		sessions: map[int64]*chatSession{},
		attempts: map[int64]*attemptTracking{},
		recent:   recentStore,
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
	now := time.Now().UTC()
	cs.mu.Lock()
	s.expireIdleSessionLocked(cs, now)
	cs.lastInteraction = now
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
	agentSession, isNewSession, err := s.ensureSession(cs)
	if err != nil {
		return err
	}

	_ = s.logger.Write("runtime", "prompt_start", map[string]any{
		"chat_id": cs.chatID,
		"source":  input.Source,
		"voice":   input.IsVoice,
		"chars":   len(input.Message),
	})

	requireTelegramSend := strings.EqualFold(strings.TrimSpace(input.Source), "telegram")
	maxAttempts := 1
	if requireTelegramSend {
		maxAttempts = 2
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		envelope := s.buildPromptEnvelope(input, isNewSession && attempt == 1)
		if attempt > 1 {
			envelope = s.buildNoSendRecoveryEnvelope(input, attempt)
			_ = s.logger.Write("runtime", "retry_prompt_after_no_send", map[string]any{
				"chat_id": cs.chatID,
				"source":  input.Source,
				"attempt": attempt,
			})
		}

		s.resetAttemptTracking(cs.chatID)
		if err := agentSession.Prompt(envelope, sdk.PromptOptions{Images: input.Images}); err != nil {
			return err
		}
		status := s.getAttemptStatus(cs.chatID)
		if !requireTelegramSend || (status.sendCalled && status.sendAfterWork) {
			_ = s.logger.Write("runtime", "prompt_end", map[string]any{
				"chat_id":  cs.chatID,
				"source":   input.Source,
				"attempts": attempt,
			})
			return nil
		}

		if !status.sendCalled {
			_ = s.logger.Write("runtime", "no_explicit_send", map[string]any{
				"chat_id": cs.chatID,
				"source":  input.Source,
				"attempt": attempt,
			})
		} else {
			_ = s.logger.Write("runtime", "send_not_final", map[string]any{
				"chat_id": cs.chatID,
				"source":  input.Source,
				"attempt": attempt,
			})
		}
	}
	status := s.getAttemptStatus(cs.chatID)
	if status.sendCalled && !status.sendAfterWork {
		return fmt.Errorf("telegram send happened before work completion; no final send after work in %d attempt(s)", maxAttempts)
	}
	return fmt.Errorf("no successful telegram send command after %d attempt(s)", maxAttempts)
}

func (s *Service) ensureSession(cs *chatSession) (*sdk.AgentSession, bool, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.session != nil {
		return cs.session, false, nil
	}

	sessionID := sessionIDForChat(cs.chatID)
	sessionPath := s.sessionPath(cs.chatID)
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

	return cs.session, true, nil
}

func (s *Service) expireIdleSessionLocked(cs *chatSession, now time.Time) {
	if cs == nil || cs.running || cs.session == nil || cs.lastInteraction.IsZero() {
		return
	}
	idle := now.Sub(cs.lastInteraction)
	if idle < sessionIdleTimeout {
		return
	}

	sessionID := sessionIDForChat(cs.chatID)
	if cs.unsubscribe != nil {
		cs.unsubscribe()
		cs.unsubscribe = nil
	}
	cs.session = nil

	sessionPath := s.sessionPath(cs.chatID)
	if err := os.Remove(sessionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = s.logger.Write("runtime", "session_close_cleanup_error", map[string]any{
			"chat_id":    cs.chatID,
			"session_id": sessionID,
			"error":      err.Error(),
		})
	}
	_ = s.logger.Write("runtime", "session_closed_idle", map[string]any{
		"chat_id":      cs.chatID,
		"session_id":   sessionID,
		"idle_seconds": int64(idle.Seconds()),
	})
}

func (s *Service) sessionPath(chatID int64) string {
	return filepath.Join(s.cfg.DataDir, "sessions", sessionIDForChat(chatID)+".jsonl")
}

func sessionIDForChat(chatID int64) string {
	return fmt.Sprintf("chat-%d", chatID)
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

func (s *Service) buildNoSendRecoveryEnvelope(input PromptInput, attempt int) string {
	loc := time.UTC
	if tz, err := time.LoadLocation(s.cfg.Timezone); err == nil {
		loc = tz
	}
	now := time.Now().In(loc)

	parts := []string{
		"[System follow-up: the previous completion ended without sending a Telegram reply.]",
		fmt.Sprintf("[Retry attempt: %d]", attempt),
		fmt.Sprintf("[Chat ID: %d]", input.ChatID),
		fmt.Sprintf("[Local time: %s]", now.Format("2006-01-02 15:04 MST")),
		fmt.Sprintf("[Repo root: %s]", s.cfg.PhiToolRoot),
		"[Requirement: treat the original user message as unresolved and execute its requested work now in this turn.]",
		"[Requirement: if the request involves code/prompt/debugging actions, run the required repo commands first, then send the final Telegram confirmation.]",
		"[Requirement: do not send an early ack before doing the requested work.]",
		"[Requirement: if you send any progress update, still send a final completion/failure Telegram message after the last work command.]",
		"[Requirement: before each Telegram reply, execute `./bin/jarvisctl telegram typing --chat <Chat ID>` and ensure a successful `./bin/jarvisctl telegram send-text --chat <Chat ID> --text ...` for user-visible output.]",
		"[When running CLI commands, use the repo root above; do not use `cd ~`.]",
		"[Do not return an empty assistant response.]",
		"[If the user just confirmed a prior yes/no question, continue with the requested action instead of asking the same confirmation again.]",
		"",
		"Original message: " + input.Message,
	}
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
		if se.Type == stream.EventToolCall {
			kind := callKindWork
			if strings.EqualFold(se.ToolName, "bash") {
				cmd, _ := se.Arguments["command"].(string)
				kind = classifyBashCallKind(cmd)
			}
			s.markPendingToolCall(chatID, se.ToolCallID, kind)
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
			toolResult := extractText(msg.ContentRaw)
			fields["tool_result"] = toolResult
			s.recordToolCallResult(chatID, ev.ToolCallID, toolResult)
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

func looksLikeTelegramTyping(cmd string) bool {
	normalized := strings.ToLower(strings.TrimSpace(cmd))
	return strings.Contains(normalized, "jarvisctl telegram typing") ||
		strings.Contains(normalized, "go run ./cmd/jarvisctl telegram typing") ||
		strings.Contains(normalized, "go run ./cmd/jarvisctl -- telegram typing")
}

func classifyBashCallKind(cmd string) callKind {
	switch {
	case looksLikeTelegramSend(cmd):
		return callKindSend
	case looksLikeTelegramTyping(cmd):
		return callKindUnknown
	default:
		return callKindWork
	}
}

func (s *Service) resetAttemptTracking(chatID int64) {
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	s.attempts[chatID] = &attemptTracking{
		pendingCalls: map[string]callKind{},
	}
}

func (s *Service) markPendingToolCall(chatID int64, toolCallID string, kind callKind) {
	if strings.TrimSpace(toolCallID) == "" {
		return
	}
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	state := s.ensureAttemptTrackingLocked(chatID)
	if state.pendingCalls == nil {
		state.pendingCalls = map[string]callKind{}
	}
	state.pendingCalls[toolCallID] = kind
}

func (s *Service) recordToolCallResult(chatID int64, toolCallID, toolResult string) {
	if strings.TrimSpace(toolCallID) == "" {
		return
	}
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	state := s.ensureAttemptTrackingLocked(chatID)
	kind, ok := state.pendingCalls[toolCallID]
	if !ok {
		return
	}
	delete(state.pendingCalls, toolCallID)

	state.sequence++
	switch kind {
	case callKindSend:
		if telegramSendSucceeded(toolResult) {
			state.sendCalled = true
			state.lastSendSeq = state.sequence
		}
	case callKindWork:
		state.lastWorkSeq = state.sequence
	}
}

func (s *Service) getAttemptStatus(chatID int64) attemptStatus {
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	state := s.ensureAttemptTrackingLocked(chatID)
	sendAfterWork := false
	if state.sendCalled {
		sendAfterWork = state.lastWorkSeq == 0 || state.lastSendSeq > state.lastWorkSeq
	}
	return attemptStatus{
		sendCalled:    state.sendCalled,
		sendAfterWork: sendAfterWork,
	}
}

func (s *Service) ensureAttemptTrackingLocked(chatID int64) *attemptTracking {
	state := s.attempts[chatID]
	if state != nil {
		return state
	}
	state = &attemptTracking{
		pendingCalls: map[string]callKind{},
	}
	s.attempts[chatID] = state
	return state
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

func telegramSendSucceeded(toolResult string) bool {
	trimmed := strings.TrimSpace(toolResult)
	if trimmed == "" {
		return false
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	for {
		var parsed any
		if err := decoder.Decode(&parsed); err != nil {
			if err == io.EOF {
				break
			}
			// Fallback for mixed outputs where JSON is embedded in plain text.
			return okTruePattern.MatchString(trimmed)
		}
		if jsonValueHasOKTrue(parsed) {
			return true
		}
	}
	return false
}

func jsonValueHasOKTrue(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if ok, exists := v["ok"].(bool); exists && ok {
			return true
		}
		for _, child := range v {
			if jsonValueHasOKTrue(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if jsonValueHasOKTrue(child) {
				return true
			}
		}
	}
	return false
}
