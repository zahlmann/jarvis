package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DefaultRecentMaxMessages = 120

type RecentStore struct {
	mu          sync.Mutex
	dir         string
	maxMessages int
	now         func() time.Time
}

type ConversationExchange struct {
	User   MessageRecord   `json:"user"`
	Jarvis []MessageRecord `json:"jarvis,omitempty"`
}

func NewRecentStore(dir string, maxMessages int) (*RecentStore, error) {
	root := strings.TrimSpace(dir)
	if root == "" {
		return nil, fmt.Errorf("recent store directory is required")
	}
	if maxMessages <= 0 {
		maxMessages = DefaultRecentMaxMessages
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &RecentStore{
		dir:         root,
		maxMessages: maxMessages,
		now:         time.Now,
	}, nil
}

func (s *RecentStore) Append(record MessageRecord) error {
	if record.ChatID == 0 {
		return fmt.Errorf("chat id is required")
	}
	record.Direction = normalizeDirection(record)
	record.Text = strings.TrimSpace(record.Text)
	if record.Timestamp == "" {
		record.Timestamp = s.now().UTC().Format(time.RFC3339Nano)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.readChatLocked(record.ChatID)
	if err != nil {
		return err
	}
	rows = append(rows, record)
	if len(rows) > s.maxMessages {
		rows = rows[len(rows)-s.maxMessages:]
	}
	return s.writeChatLocked(record.ChatID, rows)
}

func (s *RecentStore) LastMessages(chatID int64, limit int) ([]MessageRecord, error) {
	if chatID == 0 {
		return nil, fmt.Errorf("chat id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.readChatLocked(chatID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	out := make([]MessageRecord, len(rows))
	copy(out, rows)
	return out, nil
}

func (s *RecentStore) LastExchanges(chatID int64, limit int) ([]ConversationExchange, error) {
	rows, err := s.LastMessages(chatID, 0)
	if err != nil {
		return nil, err
	}
	exchanges := BuildConversationExchanges(rows)
	if limit > 0 && len(exchanges) > limit {
		exchanges = exchanges[len(exchanges)-limit:]
	}
	out := make([]ConversationExchange, len(exchanges))
	copy(out, exchanges)
	return out, nil
}

func BuildConversationExchanges(messages []MessageRecord) []ConversationExchange {
	exchanges := make([]ConversationExchange, 0)
	var current *ConversationExchange
	flush := func() {
		if current != nil {
			exchanges = append(exchanges, *current)
			current = nil
		}
	}

	for _, record := range messages {
		switch normalizeDirection(record) {
		case "inbound":
			flush()
			record.Direction = "inbound"
			current = &ConversationExchange{User: record}
		case "outbound":
			if current == nil {
				continue
			}
			record.Direction = "outbound"
			current.Jarvis = append(current.Jarvis, record)
		}
	}

	flush()
	return exchanges
}

func normalizeDirection(record MessageRecord) string {
	direction := strings.ToLower(strings.TrimSpace(record.Direction))
	switch direction {
	case "inbound", "outbound":
		return direction
	}
	if strings.EqualFold(strings.TrimSpace(record.Sender), "jarvis") {
		return "outbound"
	}
	return "inbound"
}

func (s *RecentStore) readChatLocked(chatID int64) ([]MessageRecord, error) {
	path := s.chatPath(chatID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	rows := make([]MessageRecord, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row MessageRecord
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.ChatID == 0 {
			row.ChatID = chatID
		}
		row.Direction = normalizeDirection(row)
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *RecentStore) writeChatLocked(chatID int64, rows []MessageRecord) error {
	path := s.chatPath(chatID)
	tmpPath := path + ".tmp"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			f.Close()
			_ = os.Remove(tmpPath)
			return err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *RecentStore) chatPath(chatID int64) string {
	return filepath.Join(s.dir, fmt.Sprintf("chat-%d.jsonl", chatID))
}
