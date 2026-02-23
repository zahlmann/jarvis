package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/store"
	"github.com/zahlmann/phi/coding/sdk"
)

func TestTelegramSendSucceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "single json ok true",
			input:  `{"message_id": 1, "ok": true}`,
			expect: true,
		},
		{
			name: "multiple json documents second has ok true",
			input: `{
  "limit": 8,
  "query": "hello",
  "results": []
}
{
  "message_id": 121,
  "ok": true
}`,
			expect: true,
		},
		{
			name: "mixed output with embedded ok true json",
			input: `debug: command started
{"ok": true, "message_id": 122}`,
			expect: true,
		},
		{
			name:   "single json ok false",
			input:  `{"ok": false}`,
			expect: false,
		},
		{
			name:   "empty output",
			input:  ``,
			expect: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := telegramSendSucceeded(tc.input)
			if got != tc.expect {
				t.Fatalf("telegramSendSucceeded() = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestAttemptStatusRequiresFinalSendAfterWork(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	chatID := int64(77)
	svc.resetAttemptTracking(chatID)

	svc.markPendingToolCall(chatID, "send-1", callKindSend)
	svc.recordToolCallResult(chatID, "send-1", `{"ok": true, "message_id": 1}`)

	svc.markPendingToolCall(chatID, "work-1", callKindWork)
	svc.recordToolCallResult(chatID, "work-1", "edited files")

	status := svc.getAttemptStatus(chatID)
	if !status.sendCalled {
		t.Fatalf("expected sendCalled=true after successful send")
	}
	if status.sendAfterWork {
		t.Fatalf("expected sendAfterWork=false when work happened after the only send")
	}

	svc.markPendingToolCall(chatID, "send-2", callKindSend)
	svc.recordToolCallResult(chatID, "send-2", `{"ok": true, "message_id": 2}`)

	status = svc.getAttemptStatus(chatID)
	if !status.sendCalled || !status.sendAfterWork {
		t.Fatalf("expected final send after work to satisfy attempt status, got %+v", status)
	}
}

func TestAttemptStatusIgnoresTypingForWorkOrdering(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	chatID := int64(78)
	svc.resetAttemptTracking(chatID)

	svc.markPendingToolCall(chatID, "work-1", callKindWork)
	svc.recordToolCallResult(chatID, "work-1", "ran tests")
	svc.markPendingToolCall(chatID, "typing-1", callKindUnknown)
	svc.recordToolCallResult(chatID, "typing-1", `{"ok": true}`)
	svc.markPendingToolCall(chatID, "send-1", callKindSend)
	svc.recordToolCallResult(chatID, "send-1", `{"ok": true, "message_id": 3}`)

	status := svc.getAttemptStatus(chatID)
	if !status.sendCalled || !status.sendAfterWork {
		t.Fatalf("expected sendAfterWork=true when final send follows work and typing, got %+v", status)
	}
}

func TestExpireIdleSessionLockedClosesAndResetsHistory(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	now := time.Now().UTC()
	chatID := int64(42)
	path := svc.sessionPath(chatID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"message\"}\n"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	unsubCalled := false
	cs := &chatSession{
		chatID:          chatID,
		session:         &sdk.AgentSession{},
		unsubscribe:     func() { unsubCalled = true },
		lastInteraction: now.Add(-(sessionIdleTimeout + time.Minute)),
	}

	cs.mu.Lock()
	svc.expireIdleSessionLocked(cs, now)
	cs.mu.Unlock()

	if cs.session != nil {
		t.Fatalf("expected session to be cleared")
	}
	if cs.unsubscribe != nil {
		t.Fatalf("expected unsubscribe callback to be cleared")
	}
	if !unsubCalled {
		t.Fatalf("expected unsubscribe callback to be invoked")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected session file to be removed, stat err=%v", err)
	}
}

func TestExpireIdleSessionLockedNoopBeforeTimeout(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	now := time.Now().UTC()
	chatID := int64(43)
	path := svc.sessionPath(chatID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"message\"}\n"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	unsubCalled := false
	cs := &chatSession{
		chatID:          chatID,
		session:         &sdk.AgentSession{},
		unsubscribe:     func() { unsubCalled = true },
		lastInteraction: now.Add(-(sessionIdleTimeout - time.Minute)),
	}

	cs.mu.Lock()
	svc.expireIdleSessionLocked(cs, now)
	cs.mu.Unlock()

	if cs.session == nil {
		t.Fatalf("expected session to remain active before timeout")
	}
	if cs.unsubscribe == nil {
		t.Fatalf("expected unsubscribe callback to remain set before timeout")
	}
	if unsubCalled {
		t.Fatalf("did not expect unsubscribe callback to be invoked")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected session file to remain, stat err=%v", err)
	}
}

func TestBuildPromptEnvelopeRecentRecapOnlyWhenRequested(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	recent, err := store.NewRecentStore(filepath.Join(t.TempDir(), "recent"), store.DefaultRecentMaxMessages)
	if err != nil {
		t.Fatalf("NewRecentStore() error = %v", err)
	}
	svc.recent = recent

	records := []store.MessageRecord{
		{ChatID: 99, MessageID: 1, Direction: "inbound", Sender: "user", Text: "what did we decide yesterday?"},
		{ChatID: 99, MessageID: 2, Direction: "outbound", Sender: "jarvis", Text: "we decided to ship on friday."},
		{ChatID: 99, MessageID: 3, Direction: "inbound", Sender: "user", Text: "ok remind me in the next session"},
	}
	for _, record := range records {
		if err := recent.Append(record); err != nil {
			t.Fatalf("Append(%d) error = %v", record.MessageID, err)
		}
	}

	input := PromptInput{
		ChatID:   99,
		UserName: "alex",
		Source:   "telegram",
		Message:  "ok remind me in the next session",
	}

	withRecap := svc.buildPromptEnvelope(input, true)
	if !strings.Contains(withRecap, "[Recent recap:") {
		t.Fatalf("expected recent recap in envelope, got: %s", withRecap)
	}
	if strings.Contains(withRecap, "recent 2 user: ok remind me in the next session") {
		t.Fatalf("expected current inbound message to be excluded from recap, got: %s", withRecap)
	}
	if !strings.Contains(withRecap, "recent 1 user: what did we decide yesterday?") {
		t.Fatalf("expected prior exchange to be included, got: %s", withRecap)
	}

	withoutRecap := svc.buildPromptEnvelope(input, false)
	if strings.Contains(withoutRecap, "[Recent recap:") {
		t.Fatalf("did not expect recent recap when disabled, got: %s", withoutRecap)
	}
}

func TestBuildNoSendRecoveryEnvelopePreservesExecutionIntent(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	svc.cfg.PhiToolRoot = "/repo/root"
	input := PromptInput{
		ChatID:   123,
		UserName: "alex",
		Source:   "telegram",
		Message:  "please debug this and commit the fix",
	}

	envelope := svc.buildNoSendRecoveryEnvelope(input, 2)
	required := []string{
		"[Repo root: /repo/root]",
		"treat the original user message as unresolved",
		"run the required repo commands first",
		"do not send an early ack before doing the requested work",
		"before each Telegram reply, execute `./bin/jarvisctl telegram typing --chat <Chat ID>`",
	}
	for _, snippet := range required {
		if !strings.Contains(envelope, snippet) {
			t.Fatalf("buildNoSendRecoveryEnvelope missing %q in: %s", snippet, envelope)
		}
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	root := t.TempDir()
	logger, err := logstore.New(filepath.Join(root, "logs"))
	if err != nil {
		t.Fatalf("create logstore: %v", err)
	}
	return &Service{
		cfg:      config.Config{DataDir: filepath.Join(root, "data")},
		logger:   logger,
		attempts: map[int64]*attemptTracking{},
	}
}
