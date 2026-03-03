package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestLoadWithOptionsUsesChatGPTTokenFileFallback(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	mustWriteFile(t, filepath.Join(homeDir, ".phi", "chatgpt_tokens.json"), `{
  "accessToken": "file-token",
  "accountId": "file-account"
}`)

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", "")
	t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "")
	t.Setenv("JARVIS_PHI_DATA_DIR", filepath.Join(t.TempDir(), "data"))

	cfg, err := LoadWithOptions(LoadOptions{
		RequireTelegramToken:  false,
		RequirePhiCredentials: false,
	})
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}
	if cfg.PhiAccessToken != "file-token" {
		t.Fatalf("PhiAccessToken=%q want=%q", cfg.PhiAccessToken, "file-token")
	}
	if cfg.PhiAccountID != "file-account" {
		t.Fatalf("PhiAccountID=%q want=%q", cfg.PhiAccountID, "file-account")
	}
}

func TestLoadWithOptionsPrefersEnvOverChatGPTTokenFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	mustWriteFile(t, filepath.Join(homeDir, ".phi", "chatgpt_tokens.json"), `{
  "accessToken": "file-token",
  "accountId": "file-account"
}`)

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PHI_CHATGPT_ACCESS_TOKEN", "env-token")
	t.Setenv("PHI_CHATGPT_ACCOUNT_ID", "env-account")
	t.Setenv("JARVIS_PHI_DATA_DIR", filepath.Join(t.TempDir(), "data"))

	cfg, err := LoadWithOptions(LoadOptions{
		RequireTelegramToken:  false,
		RequirePhiCredentials: false,
	})
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}
	if cfg.PhiAccessToken != "env-token" {
		t.Fatalf("PhiAccessToken=%q want=%q", cfg.PhiAccessToken, "env-token")
	}
	if cfg.PhiAccountID != "env-account" {
		t.Fatalf("PhiAccountID=%q want=%q", cfg.PhiAccountID, "env-account")
	}
}

func TestDefaultPromptBehaviorChangesStayOutOfMemory(t *testing.T) {
	prompt := defaultPrompt("alex")
	required := []string{
		"change your own behavior",
		"worth looking up later",
		"internal/config/config.go",
		"defaultPrompt(...)",
		"Do not save that request as memory",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("defaultPrompt missing %q", fragment)
		}
	}
}

func TestDefaultPromptStandaloneWorkspaceRules(t *testing.T) {
	prompt := defaultPrompt("alex")
	required := []string{
		"`scratch/` at repo root",
		"check `scratch/` first",
		"own subdirectory under `scratch/`",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("defaultPrompt missing %q", fragment)
		}
	}
}

func TestDefaultPromptTypingAndFormattingPreferences(t *testing.T) {
	prompt := defaultPrompt("alex")
	required := []string{
		"All user-visible replies must be sent through `./bin/jarvisctl telegram ...` executed from bash.",
		"Treat each inbound Telegram message as unresolved until a successful `send-text --chat --text` completes in the same turn.",
		"Do not rely on a retry turn; finish the requested work and deliver the final Telegram reply in one pass whenever possible.",
		"Before each Telegram reply, always send typing status first",
		"`./bin/jarvisctl telegram typing --chat <Chat ID>`",
		"Assistant final responses are internal and are not delivered to Telegram automatically.",
		"If you do not run a telegram send command, the user receives nothing.",
		"For `jarvisctl`, prefer subcommand help (for example `./bin/jarvisctl schedule add --help`)",
		"embed inline code snippets with single backticks",
		"triple-backtick fences",
		"quote the full `--text` payload safely",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("defaultPrompt missing %q", fragment)
		}
	}
}

func TestDefaultPromptRecentRecapCommand(t *testing.T) {
	prompt := defaultPrompt("alex")
	required := []string{
		"implicitly references very recent chat context",
		"`./bin/jarvisctl recent --chat <Chat ID> --pairs 10`",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("defaultPrompt missing %q", fragment)
		}
	}
}

func TestDefaultPromptActionRequestCompletionGuidance(t *testing.T) {
	prompt := defaultPrompt("alex")
	required := []string{
		"Do not use `cd ~` for repo tasks",
		"avoid placeholder-only updates like on it/done before execution",
		"send one concise completion message with concrete outcomes",
		"full permission to read, edit, create, delete, commit, and push",
		"always use uv-based workflows",
		"Do not use plain `python` or `pip` commands unless uv is unavailable",
		"avoid broad recursive scans over `.cache/`, `.git/`, `data/`, or `bin/`",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("defaultPrompt missing %q", fragment)
		}
	}
}

func TestDefaultToolRootPrefersRepoLikeCWD(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n")
	mustWriteFile(t, filepath.Join(root, "cmd", "server", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "cmd", "jarvisctl", "main.go"), "package main\n")

	got := defaultToolRoot(root, func() (string, error) { return "", os.ErrNotExist })
	if got != root {
		t.Fatalf("defaultToolRoot() = %q, want %q", got, root)
	}
}

func TestDefaultToolRootFallsBackToExecutableParent(t *testing.T) {
	cwd := t.TempDir()
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "go.mod"), "module example.com/test\n")
	mustWriteFile(t, filepath.Join(repo, "cmd", "server", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(repo, "cmd", "jarvisctl", "main.go"), "package main\n")
	exe := filepath.Join(repo, "bin", "jarvis-phi-server")
	mustWriteFile(t, exe, "")

	got := defaultToolRoot(cwd, func() (string, error) { return exe, nil })
	if got != repo {
		t.Fatalf("defaultToolRoot() = %q, want %q", got, repo)
	}
}

func TestDefaultToolRootReturnsCWDWhenNoSignals(t *testing.T) {
	cwd := t.TempDir()
	exe := filepath.Join(t.TempDir(), "bin", "jarvis-phi-server")
	mustWriteFile(t, exe, "")

	got := defaultToolRoot(cwd, func() (string, error) { return exe, nil })
	if got != cwd {
		t.Fatalf("defaultToolRoot() = %q, want %q", got, cwd)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
