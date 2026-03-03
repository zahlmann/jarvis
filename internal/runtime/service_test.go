package runtime

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/store"
	"github.com/zahlmann/phi/ai/model"
)

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
	sessionID := "session-77"
	svc.resetAttemptTracking(sessionID)

	svc.markPendingToolCall(sessionID, "send-1", callKindSend)
	svc.recordToolCallResult(sessionID, "send-1", "bash", textMessage(`{"ok": true, "message_id": 1}`), false)

	svc.markPendingToolCall(sessionID, "work-1", callKindWork)
	svc.recordToolCallResult(sessionID, "work-1", "bash", textMessage("edited files"), false)

	status := svc.getAttemptStatus(sessionID)
	if !status.sendCalled {
		t.Fatalf("expected sendCalled=true after successful send")
	}
	if status.sendAfterWork {
		t.Fatalf("expected sendAfterWork=false when work happened after the only send")
	}

	svc.markPendingToolCall(sessionID, "send-2", callKindSend)
	svc.recordToolCallResult(sessionID, "send-2", "bash", textMessage(`{"ok": true, "message_id": 2}`), false)

	status = svc.getAttemptStatus(sessionID)
	if !status.sendCalled || !status.sendAfterWork {
		t.Fatalf("expected final send after work to satisfy attempt status, got %+v", status)
	}
}

func TestAttemptStatusIgnoresTypingForWorkOrdering(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	sessionID := "session-78"
	svc.resetAttemptTracking(sessionID)

	svc.markPendingToolCall(sessionID, "work-1", callKindWork)
	svc.recordToolCallResult(sessionID, "work-1", "bash", textMessage("ran tests"), false)

	svc.markPendingToolCall(sessionID, "typing-1", callKindUnknown)
	svc.recordToolCallResult(sessionID, "typing-1", "bash", textMessage(`{"ok": true, "action": "typing"}`), false)

	svc.markPendingToolCall(sessionID, "send-1", callKindSend)
	svc.recordToolCallResult(sessionID, "send-1", "bash", textMessage(`{"ok": true, "message_id": 3}`), false)

	status := svc.getAttemptStatus(sessionID)
	if !status.sendCalled || !status.sendAfterWork {
		t.Fatalf("expected sendAfterWork=true when final send follows work and typing, got %+v", status)
	}
}

func TestRequiresTelegramSend(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source string
		want   bool
	}{
		{source: "telegram", want: true},
		{source: " schedule:job-1 ", want: true},
		{source: "heartbeat", want: false},
		{source: "cron", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(strings.TrimSpace(tc.source), func(t *testing.T) {
			t.Parallel()
			got := requiresTelegramSend(tc.source)
			if got != tc.want {
				t.Fatalf("requiresTelegramSend(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func textMessage(text string) *model.Message {
	return &model.Message{
		Role: model.RoleToolResult,
		ContentRaw: []any{
			model.TextContent{Type: model.ContentText, Text: text},
		},
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
		cfg:           config.Config{DataDir: filepath.Join(root, "data")},
		logger:        logger,
		sessions:      map[int64]*chatSession{},
		sessionToChat: map[string]int64{},
		finalSeq:      map[string]int64{},
		finalWaiter:   map[string][]chan struct{}{},
		attempts:      map[string]*attemptTracking{},
	}
}
