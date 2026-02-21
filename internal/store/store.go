package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DedupStore struct {
	mu   sync.Mutex
	path string
	seen map[string]string
}

func NewDedupStore(path string) (*DedupStore, error) {
	d := &DedupStore{path: path, seen: map[string]string{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := d.load(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DedupStore) load() error {
	data, err := os.ReadFile(d.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &d.seen)
}

func (d *DedupStore) save() error {
	payload, err := json.MarshalIndent(d.seen, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.path, payload, 0o644)
}

func (d *DedupStore) Seen(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[id]
	return ok
}

func (d *DedupStore) Mark(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = time.Now().UTC().Format(time.RFC3339Nano)
	return d.save()
}

type MessageRecord struct {
	ChatID    int64  `json:"chatId"`
	MessageID int64  `json:"messageId"`
	Direction string `json:"direction"`
	Sender    string `json:"sender"`
	Text      string `json:"text,omitempty"`
	Timestamp string `json:"timestamp"`
}

type MessageIndex struct {
	mu      sync.Mutex
	path    string
	records map[string]MessageRecord
}

func NewMessageIndex(path string) (*MessageIndex, error) {
	m := &MessageIndex{path: path, records: map[string]MessageRecord{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *MessageIndex) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &m.records)
}

func (m *MessageIndex) save() error {
	payload, err := json.MarshalIndent(m.records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, payload, 0o644)
}

func (m *MessageIndex) Put(r MessageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Timestamp == "" {
		r.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	m.records[key(r.ChatID, r.MessageID)] = r
	return m.save()
}

func (m *MessageIndex) Get(chatID, messageID int64) (MessageRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[key(chatID, messageID)]
	return r, ok
}

func key(chatID, messageID int64) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}
