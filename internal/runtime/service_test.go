package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
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

func newTestService(t *testing.T) *Service {
	t.Helper()

	root := t.TempDir()
	logger, err := logstore.New(filepath.Join(root, "logs"))
	if err != nil {
		t.Fatalf("create logstore: %v", err)
	}
	return &Service{
		cfg:    config.Config{DataDir: filepath.Join(root, "data")},
		logger: logger,
	}
}
