package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	parquet "github.com/parquet-go/parquet-go"
)

type Record struct {
	ID        string    `json:"id" parquet:"id,zstd"`
	Keywords  []string  `json:"keywords" parquet:"keywords,zstd"`
	Memory    string    `json:"memory" parquet:"memory,zstd"`
	CreatedAt string    `json:"created_at" parquet:"created_at"`
	Embedding []float32 `json:"embedding,omitempty" parquet:"embedding"`
}

type SearchResult struct {
	ID        string   `json:"id"`
	Keywords  []string `json:"keywords"`
	Memory    string   `json:"memory"`
	CreatedAt string   `json:"created_at"`
	Score     float64  `json:"score"`
}

type Store struct {
	mu       sync.Mutex
	path     string
	lockPath string
}

func NewStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("memory store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	s := &Store{
		path:     path,
		lockPath: path + ".lock",
	}

	err := s.withFileLock(func() error {
		if _, statErr := os.Stat(s.path); statErr == nil {
			return nil
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		return s.writeRowsUnlocked([]Record{})
	})
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Save(keywords []string, fullMemory string, createdAt time.Time) (Record, error) {
	cleanKeywords := NormalizeKeywords(keywords)
	memoryText := strings.TrimSpace(fullMemory)
	if len(cleanKeywords) == 0 {
		return Record{}, fmt.Errorf("at least one keyword is required")
	}
	if memoryText == "" {
		return Record{}, fmt.Errorf("memory text is required")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	record := Record{
		ID:        newRecordID(),
		Keywords:  cleanKeywords,
		Memory:    memoryText,
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
		Embedding: []float32{},
	}

	if err := s.withFileLock(func() error {
		rows, err := s.readRowsUnlocked()
		if err != nil {
			return err
		}
		rows = append(rows, record)
		return s.writeRowsUnlocked(rows)
	}); err != nil {
		return Record{}, err
	}

	return record, nil
}

func (s *Store) Remove(id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, fmt.Errorf("id is required")
	}

	removed := false
	err := s.withFileLock(func() error {
		rows, err := s.readRowsUnlocked()
		if err != nil {
			return err
		}

		next := make([]Record, 0, len(rows))
		for _, row := range rows {
			if row.ID == id {
				removed = true
				continue
			}
			next = append(next, row)
		}
		if !removed {
			return nil
		}
		return s.writeRowsUnlocked(next)
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (s *Store) List() ([]Record, error) {
	rows := []Record{}
	if err := s.withFileLock(func() error {
		loaded, err := s.readRowsUnlocked()
		if err != nil {
			return err
		}
		rows = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].CreatedAt > rows[j].CreatedAt
	})
	return rows, nil
}

func (s *Store) Search(queryEmbedding []float32, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	normalizedQuery, err := NormalizeEmbedding(queryEmbedding)
	if err != nil {
		return nil, err
	}

	rows := []Record{}
	if err := s.withFileLock(func() error {
		loaded, readErr := s.readRowsUnlocked()
		if readErr != nil {
			return readErr
		}
		rows = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		if len(row.Embedding) == 0 || len(row.Embedding) != len(normalizedQuery) {
			continue
		}
		score := DotProduct(normalizedQuery, row.Embedding)
		results = append(results, SearchResult{
			ID:        row.ID,
			Keywords:  append([]string{}, row.Keywords...),
			Memory:    row.Memory,
			CreatedAt: row.CreatedAt,
			Score:     score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].CreatedAt > results[j].CreatedAt
		}
		return results[i].Score > results[j].Score
	})

	if limit < len(results) {
		results = results[:limit]
	}
	return results, nil
}

func (s *Store) BackfillEmbeddings(ctx context.Context, embedder Embedder, batchSize int) (int, error) {
	if embedder == nil {
		return 0, fmt.Errorf("embedder is required")
	}
	if batchSize <= 0 {
		batchSize = 25
	}

	type pendingRow struct {
		ID         string
		KeywordRaw string
	}
	pending := make([]pendingRow, 0, batchSize)

	err := s.withFileLock(func() error {
		rows, readErr := s.readRowsUnlocked()
		if readErr != nil {
			return readErr
		}
		for _, row := range rows {
			if len(row.Embedding) > 0 {
				continue
			}
			joined := strings.TrimSpace(strings.Join(row.Keywords, ", "))
			if joined == "" {
				continue
			}
			pending = append(pending, pendingRow{ID: row.ID, KeywordRaw: joined})
			if len(pending) >= batchSize {
				break
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	updates := make(map[string][]float32, len(pending))
	for _, row := range pending {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		embedding, embedErr := embedder.Embed(ctx, row.KeywordRaw)
		if embedErr != nil {
			return 0, embedErr
		}
		updates[row.ID] = embedding
	}

	updated := 0
	err = s.withFileLock(func() error {
		rows, readErr := s.readRowsUnlocked()
		if readErr != nil {
			return readErr
		}
		for i := range rows {
			if len(rows[i].Embedding) > 0 {
				continue
			}
			if emb, ok := updates[rows[i].ID]; ok {
				rows[i].Embedding = emb
				updated++
			}
		}
		if updated == 0 {
			return nil
		}
		return s.writeRowsUnlocked(rows)
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}

func (s *Store) readRowsUnlocked() ([]Record, error) {
	rows, err := parquet.ReadFile[Record](s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Record{}, nil
		}
		return nil, err
	}
	if rows == nil {
		return []Record{}, nil
	}
	for i := range rows {
		rows[i].Keywords = NormalizeKeywords(rows[i].Keywords)
		rows[i].Embedding = append([]float32{}, rows[i].Embedding...)
	}
	return rows, nil
}

func (s *Store) writeRowsUnlocked(rows []Record) error {
	if rows == nil {
		rows = []Record{}
	}
	tmp := s.path + ".tmp"
	if err := parquet.WriteFile(tmp, rows); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) withFileLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lockFile, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return fn()
}

func NormalizeKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return []string{}
	}

	out := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, raw := range keywords {
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			k := strings.ToLower(strings.TrimSpace(part))
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

func newRecordID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("mem-%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("mem-%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(buf))
}
