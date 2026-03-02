package runtime

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/store"
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
	}
}
