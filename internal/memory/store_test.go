package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type fakeEmbedder struct {
	vectors map[string][]float32
}

func (f fakeEmbedder) Embed(_ context.Context, input string) ([]float32, error) {
	v, ok := f.vectors[input]
	if !ok {
		return nil, errMissingVector
	}
	return NormalizeEmbedding(v)
}

var errMissingVector = &testError{"missing fake embedding vector"}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }

func TestStoreSaveListSearchRemove(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "memories.parquet")
	st, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	first, err := st.Save([]string{"coffee", "preferences"}, "User prefers black coffee in the morning.", time.Now().UTC())
	if err != nil {
		t.Fatalf("Save first failed: %v", err)
	}
	second, err := st.Save([]string{"travel", "japan"}, "User wants to visit Tokyo in spring.", time.Now().UTC())
	if err != nil {
		t.Fatalf("Save second failed: %v", err)
	}

	updated, err := st.BackfillEmbeddings(context.Background(), fakeEmbedder{
		vectors: map[string][]float32{
			"coffee, preferences": {1, 0},
			"travel, japan":       {0, 1},
		},
	}, 10)
	if err != nil {
		t.Fatalf("BackfillEmbeddings failed: %v", err)
	}
	if updated != 2 {
		t.Fatalf("BackfillEmbeddings updated=%d want=2", updated)
	}

	query, err := NormalizeEmbedding([]float32{1, 0})
	if err != nil {
		t.Fatalf("NormalizeEmbedding failed: %v", err)
	}
	results, err := st.Search(query, 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search len=%d want=2", len(results))
	}
	if results[0].ID != first.ID {
		t.Fatalf("Search top result id=%s want=%s", results[0].ID, first.ID)
	}

	rows, err := st.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List len=%d want=2", len(rows))
	}

	removed, err := st.Remove(second.ID)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if !removed {
		t.Fatalf("Remove returned removed=false for existing row")
	}

	rows, err = st.List()
	if err != nil {
		t.Fatalf("List after remove failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List after remove len=%d want=1", len(rows))
	}
	if rows[0].ID != first.ID {
		t.Fatalf("Remaining row id=%s want=%s", rows[0].ID, first.ID)
	}
}

func TestNormalizeKeywords(t *testing.T) {
	got := NormalizeKeywords([]string{"  coffee , tea", "Tea", "work", "", " coffee "})
	want := []string{"coffee", "tea", "work"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeKeywords len=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeKeywords[%d]=%q want=%q", i, got[i], want[i])
		}
	}
}
