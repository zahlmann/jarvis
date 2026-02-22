package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestRecentStoreAppendTrimAndLimit(t *testing.T) {
	t.Parallel()

	st, err := NewRecentStore(filepath.Join(t.TempDir(), "recent"), 3)
	if err != nil {
		t.Fatalf("NewRecentStore() error = %v", err)
	}

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	counter := 0
	st.now = func() time.Time {
		counter++
		return base.Add(time.Duration(counter) * time.Second)
	}

	for i := 1; i <= 4; i++ {
		if err := st.Append(MessageRecord{
			ChatID:    123,
			MessageID: int64(i),
			Direction: "inbound",
			Sender:    "user",
			Text:      fmt.Sprintf("m%d", i),
		}); err != nil {
			t.Fatalf("Append(%d) error = %v", i, err)
		}
	}

	rows, err := st.LastMessages(123, 0)
	if err != nil {
		t.Fatalf("LastMessages() error = %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].MessageID != 2 || rows[2].MessageID != 4 {
		t.Fatalf("unexpected trimmed message ids: first=%d last=%d", rows[0].MessageID, rows[2].MessageID)
	}

	limited, err := st.LastMessages(123, 2)
	if err != nil {
		t.Fatalf("LastMessages(limit) error = %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("len(limited) = %d, want 2", len(limited))
	}
	if limited[0].MessageID != 3 || limited[1].MessageID != 4 {
		t.Fatalf("unexpected limited message ids: %d, %d", limited[0].MessageID, limited[1].MessageID)
	}
}

func TestRecentStoreLastExchanges(t *testing.T) {
	t.Parallel()

	st, err := NewRecentStore(filepath.Join(t.TempDir(), "recent"), 20)
	if err != nil {
		t.Fatalf("NewRecentStore() error = %v", err)
	}

	records := []MessageRecord{
		{ChatID: 55, MessageID: 1, Direction: "inbound", Sender: "alex", Text: "first"},
		{ChatID: 55, MessageID: 2, Direction: "outbound", Sender: "jarvis", Text: "r1"},
		{ChatID: 55, MessageID: 3, Direction: "outbound", Sender: "jarvis", Text: "r1b"},
		{ChatID: 55, MessageID: 4, Direction: "inbound", Sender: "alex", Text: "second"},
		{ChatID: 55, MessageID: 5, Direction: "outbound", Sender: "jarvis", Text: "r2"},
		{ChatID: 55, MessageID: 6, Direction: "inbound", Sender: "alex", Text: "third"},
	}
	for _, record := range records {
		if err := st.Append(record); err != nil {
			t.Fatalf("Append(%d) error = %v", record.MessageID, err)
		}
	}

	exchanges, err := st.LastExchanges(55, 10)
	if err != nil {
		t.Fatalf("LastExchanges() error = %v", err)
	}
	if len(exchanges) != 3 {
		t.Fatalf("len(exchanges) = %d, want 3", len(exchanges))
	}
	if exchanges[0].User.Text != "first" || len(exchanges[0].Jarvis) != 2 {
		t.Fatalf("unexpected first exchange: %#v", exchanges[0])
	}
	if exchanges[2].User.Text != "third" || len(exchanges[2].Jarvis) != 0 {
		t.Fatalf("unexpected third exchange: %#v", exchanges[2])
	}

	limited, err := st.LastExchanges(55, 2)
	if err != nil {
		t.Fatalf("LastExchanges(limit) error = %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("len(limited) = %d, want 2", len(limited))
	}
	if limited[0].User.Text != "second" || limited[1].User.Text != "third" {
		t.Fatalf("unexpected limited exchanges: %#v", limited)
	}
}
