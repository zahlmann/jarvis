package parallel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatalf("expected missing api key error")
	}
	if !strings.Contains(err.Error(), "PARALLEL_API_KEY is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientSearchSendsExpectedRequest(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAPIKey string
	var gotPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"search_id": "search_123",
			"results":   []map[string]any{{"title": "ok"}},
		})
	}))
	defer server.Close()

	client, err := newClient("parallel-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("newClient failed: %v", err)
	}

	resp, err := client.Search(context.Background(), map[string]any{"objective": "latest ai funding"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if gotPath != "/v1beta/search" {
		t.Fatalf("path=%q want=%q", gotPath, "/v1beta/search")
	}
	if gotAPIKey != "parallel-key" {
		t.Fatalf("x-api-key=%q want=%q", gotAPIKey, "parallel-key")
	}
	if gotPayload["objective"] != "latest ai funding" {
		t.Fatalf("objective=%v want=%q", gotPayload["objective"], "latest ai funding")
	}
	if resp["search_id"] != "search_123" {
		t.Fatalf("search_id=%v want=%q", resp["search_id"], "search_123")
	}
}

func TestClientExtractReturnsStatusErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/extract" {
			t.Fatalf("path=%q want=%q", r.URL.Path, "/v1beta/extract")
		}
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := newClient("parallel-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("newClient failed: %v", err)
	}

	_, err = client.Extract(context.Background(), map[string]any{
		"urls": []string{"https://example.com"},
	})
	if err == nil {
		t.Fatalf("expected extract error")
	}
	if !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected status code in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("expected response body in error, got: %v", err)
	}
}
