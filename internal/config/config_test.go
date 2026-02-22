package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zahlmann/phi/agent"
)

func TestParseThinkingLevel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want agent.ThinkingLevel
	}{
		{name: "none", raw: "none", want: agent.ThinkingNone},
		{name: "minimal", raw: "minimal", want: agent.ThinkingMinimal},
		{name: "low", raw: "low", want: agent.ThinkingLow},
		{name: "medium", raw: "medium", want: agent.ThinkingMedium},
		{name: "high", raw: "high", want: agent.ThinkingHigh},
		{name: "xhigh", raw: "xhigh", want: agent.ThinkingXHigh},
		{name: "unknown defaults xhigh", raw: "bogus", want: agent.ThinkingXHigh},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseThinkingLevel(tc.raw); got != tc.want {
				t.Fatalf("parseThinkingLevel(%q): got=%q want=%q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadWithOptionsRequiresOpenAIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("JARVIS_PHI_DATA_DIR", filepath.Join(t.TempDir(), "data"))

	_, err := LoadWithOptions(LoadOptions{
		RequireTelegramToken:  false,
		RequirePhiCredentials: false,
	})
	if err == nil {
		t.Fatalf("expected error when OPENAI_API_KEY is empty")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY is required") {
		t.Fatalf("expected OPENAI_API_KEY error, got: %v", err)
	}
}

func TestLoadWithOptionsMemoryEmbeddingModelDefault(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("JARVIS_PHI_MEMORY_EMBEDDING_MODEL", "")
	t.Setenv("JARVIS_PHI_DATA_DIR", filepath.Join(t.TempDir(), "data"))

	cfg, err := LoadWithOptions(LoadOptions{
		RequireTelegramToken:  false,
		RequirePhiCredentials: false,
	})
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}
	if cfg.MemoryEmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("MemoryEmbeddingModel=%q want=%q", cfg.MemoryEmbeddingModel, "text-embedding-3-small")
	}
}
