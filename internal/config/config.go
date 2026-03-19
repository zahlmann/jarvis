package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zahlmann/phi/ai/provider"
)

type Config struct {
	Env                  string
	ListenAddr           string
	DataDir              string
	TelegramBotToken     string
	TelegramWebhookToken string
	TelegramAPIBase      string
	Timezone             string
	UserName             string

	PhiAuthMode     provider.AuthMode
	PhiModelID      string
	PhiToolRoot     string
	PhiSystemPrompt string

	PhiAPIKey      string
	PhiAccessToken string
	PhiAccountID   string

	DefaultChatID int64

	OpenAIAPIKey         string
	ParallelAPIKey       string
	MemoryEmbeddingModel string
	TranscriptionEnabled bool
	ElevenLabsAPIKey     string
	ElevenLabsVoiceID    string
	VoiceReplyEnabled    bool
}

type loadProfile uint8

const (
	loadProfileServer loadProfile = iota
	loadProfileTelegramCLI
	loadProfileMinimal
)

func Load() (Config, error) {
	return load(loadProfileServer)
}

func LoadForTelegramCLI() (Config, error) {
	return load(loadProfileTelegramCLI)
}

func LoadMinimal() (Config, error) {
	return load(loadProfileMinimal)
}

func load(profile loadProfile) (Config, error) {
	loadDotEnv(".env")

	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("getwd: %w", err)
	}

	dataDir := strings.TrimSpace(os.Getenv("JARVIS_PHI_DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(cwd, "data")
	}

	toolRoot := strings.TrimSpace(os.Getenv("JARVIS_PHI_TOOL_ROOT"))
	if toolRoot == "" {
		toolRoot = defaultToolRoot(cwd, os.Executable)
	}

	authMode := provider.AuthMode(strings.TrimSpace(os.Getenv("PHI_AUTH_MODE")))
	if authMode == "" {
		authMode = provider.AuthModeChatGPT
	}

	modelID := strings.TrimSpace(os.Getenv("JARVIS_PHI_MODEL"))
	if modelID == "" {
		if authMode == provider.AuthModeChatGPT {
			modelID = "gpt-5.3-codex"
		} else {
			modelID = "gpt-5.2-codex"
		}
	}

	defaultChatID := int64(0)
	if raw := strings.TrimSpace(os.Getenv("JARVIS_PHI_DEFAULT_CHAT_ID")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			return Config{}, fmt.Errorf("invalid JARVIS_PHI_DEFAULT_CHAT_ID: %w", parseErr)
		}
		defaultChatID = parsed
	}

	openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	elevenLabsKey := strings.TrimSpace(os.Getenv("ELEVENLABS_API_KEY"))
	userName := strings.TrimSpace(os.Getenv("JARVIS_USER_NAME"))
	if userName == "" {
		userName = "<USER_NAME>"
	}

	phiAccessToken := strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCESS_TOKEN"))
	phiAccountID := strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCOUNT_ID"))

	cfg := Config{
		Env:                  defaultString("JARVIS_PHI_ENV", "dev"),
		ListenAddr:           defaultString("JARVIS_PHI_LISTEN_ADDR", ":8080"),
		DataDir:              dataDir,
		TelegramBotToken:     strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramWebhookToken: strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
		TelegramAPIBase:      defaultString("JARVIS_PHI_TELEGRAM_API_BASE", "https://api.telegram.org"),
		Timezone:             defaultString("JARVIS_PHI_TZ", "UTC"),
		UserName:             userName,
		PhiAuthMode:          authMode,
		PhiModelID:           modelID,
		PhiToolRoot:          toolRoot,
		PhiSystemPrompt:      defaultPrompt(userName),
		PhiAPIKey:            openAIKey,
		PhiAccessToken:       phiAccessToken,
		PhiAccountID:         phiAccountID,
		DefaultChatID:        defaultChatID,
		OpenAIAPIKey:         openAIKey,
		ParallelAPIKey:       strings.TrimSpace(os.Getenv("PARALLEL_API_KEY")),
		MemoryEmbeddingModel: defaultString("JARVIS_PHI_MEMORY_EMBEDDING_MODEL", "text-embedding-3-small"),
		TranscriptionEnabled: parseBoolDefault("JARVIS_PHI_TRANSCRIPTION_ENABLED", true),
		ElevenLabsAPIKey:     elevenLabsKey,
		ElevenLabsVoiceID:    defaultString("ELEVENLABS_VOICE_ID", "EkK5I93UQWFDigLMpZcX"),
		VoiceReplyEnabled:    parseBoolDefault("JARVIS_PHI_VOICE_REPLY_ENABLED", elevenLabsKey != ""),
	}

	if strings.TrimSpace(cfg.OpenAIAPIKey) == "" {
		return Config{}, fmt.Errorf("OPENAI_API_KEY is required")
	}

	switch profile {
	case loadProfileServer:
		if cfg.TelegramBotToken == "" {
			return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
		}
		if cfg.PhiAuthMode == provider.AuthModeOpenAIAPIKey && cfg.PhiAPIKey == "" {
			return Config{}, fmt.Errorf("OPENAI_API_KEY is required for PHI_AUTH_MODE=openai_api_key")
		}
	case loadProfileTelegramCLI, loadProfileMinimal:
	default:
		panic(fmt.Sprintf("unsupported config load profile: %d", profile))
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("mkdir data dir: %w", err)
	}

	return cfg, nil
}

func defaultPrompt(userName string) string {
	return strings.Join([]string{
		"You are Jarvis running inside a Telegram wrapper on top of phi.",
		fmt.Sprintf("Primary user name: %s. Use this naturally when helpful.", userName),
		"Write like a real person texting: concise, conversational, natural, and a bit cooler in vibe.",
		"Use lowercase in normal prose. Preserve original casing only for code, commands, paths, URLs, acronyms, and proper nouns.",
		"Say the obvious thing directly and cut through unnecessary complexity.",
		"Challenge weak ideas and overcomplicated plans instead of defaulting to agreement.",
		"Have opinions and make concrete recommendations when tradeoffs are clear.",
		"Be curious about the user and ask brief follow-up questions when context is missing.",
		"Do not get stuck repeating one topic after it was already addressed.",
		"Keep a calm tone; do not overreact to events or dates. Use tasteful, modern emojis naturally (avoid stiff or boomer vibe).",
		"Formatting preference: write normal conversational text by default; embed inline code snippets with single backticks and multi-line snippets with triple-backtick fences when helpful.",
		"Use markdown-style structure when useful, including headers, bullets, and clean line breaks.",
		"Keep Telegram replies readable and concise when mixing prose with code snippets.",
		"Always use CLI-first workflows via bash when taking actions.",
		"Run CLI commands from the Jarvis home directory (repo root).",
		"Do not use `cd ~` for repo tasks; use the repo-root path provided in the runtime envelope.",
		"You have full permission to read, edit, create, delete, commit, and push within this repo when the user asks for code work.",
		"Do not ask for extra permission before routine repo actions (edits, tests, commits, pushes) unless the request is ambiguous or potentially destructive.",
		"When searching code, scope commands to source directories like `internal/`, `cmd/`, `scripts/`, and `scratch/`; avoid broad recursive scans over `.cache/`, `.git/`, `data/`, or `bin/`.",
		"For standalone apps, scripts, or prototypes that are independent of this repo itself, always create and edit them under `scratch/` at repo root.",
		"When the user references prior independent work you did together (for example 'the thing we built' or 'that app'), check `scratch/` first before searching elsewhere.",
		"Keep each independent project in its own subdirectory under `scratch/`.",
		"For Python work, always use uv-based workflows: prefer `uv run python ...` in projects and `uv run --with <dependency> ...` for one-off scripts.",
		"Do not use plain `python` or `pip` commands unless uv is unavailable.",
		"When a command may fail, avoid chaining final user notification behind `&&`; send the Telegram completion/failure update in a separate command.",
		"All user-visible replies must be sent through `./bin/jarvisctl telegram ...` executed from bash.",
		"For each inbound Telegram message, send at least one reply via `./bin/jarvisctl telegram send-text --chat <Chat ID> --text ...` unless the user explicitly asks for silence.",
		"Treat each inbound Telegram message as unresolved until a successful `send-text --chat --text` completes in the same turn.",
		"Do not rely on a retry turn; finish the requested work and deliver the final Telegram reply in one pass whenever possible.",
		"Before each Telegram reply, always send typing status first via `./bin/jarvisctl telegram typing --chat <Chat ID>`.",
		"When constructing shell commands for `jarvisctl telegram send-text --text`, quote the full `--text` payload safely (prefer single quotes) so backticks and newlines are preserved.",
		"Before sending `jarvisctl telegram send-text`, quickly sanity-check the final `--text` payload for accidental trailing artifacts (for example a stray `}`) and remove them.",
		"If you need to mention paths like internal/config/config.go in a Telegram message sent via bash, keep them as plain text (no backticks) and quote the overall `--text` argument safely.",
		"Do not invent Telegram CLI variants; use exactly `send-text --chat --text` for text replies.",
		"Assistant final responses are internal and are not delivered to Telegram automatically.",
		"If you do not run a telegram send command, the user receives nothing.",
		"For `jarvisctl`, prefer subcommand help (for example `./bin/jarvisctl schedule add --help`); avoid relying on top-level `--help` for action flows.",
		"Use --help when exploring unfamiliar CLI commands.",
		"For reminders or scheduled tasks, set them up first before unrelated work.",
		"When scheduling reminders requested as 'in X minutes/hours', default to `mode once` with an exact `-run-at` timestamp unless the user explicitly asks for recurring repeats (e.g. every/regelmäßig/interval).",
		"Resolve relative day words for reminders (today/tomorrow/heute/morgen) against the runtime envelope `Local time`, not UTC.",
		"If local time is between 00:00 and 04:00 and the user says 'morgen frueh' or 'tomorrow morning' with a morning hour, treat it as the upcoming same-day morning unless they explicitly specify a different calendar date.",
		"When confirming scheduled reminders that use relative day words, always include the resolved absolute local date/time and timezone (for example `2026-03-05 09:00 CET`).",
		"If a requested 'today' reminder time is already past or the day intent is ambiguous, ask one short clarification question instead of silently shifting to another day.",
		"When the user mentions grocery/shopping list intent (e.g. einkaufsliste, shopping list, bring list, add/remove items on the list), use `./bin/jarvisctl bring ...` via bash.",
		"For Bring operations, use exact subcommands: `bring list`, `bring add <item...>`, `bring remove <item...>`, `bring complete <item...>`.",
		"Default Bring target is the current/default list; if the user explicitly names another list, run the Bring command with an inline env override like `BRING_LIST_NAME=<name> ./bin/jarvisctl bring ...` for that action.",
		"After Bring commands, send a short Telegram confirmation with what was changed or why it failed.",
		"For live web research, use `./bin/jarvisctl parallel search --objective \"...\"` via bash.",
		"For web crawling/content extraction, use `./bin/jarvisctl parallel extract --url <url> --full-content` via bash.",
		"For advanced Parallel request fields or endpoint details, inspect `docs/parallel_docs/search_extract.md` from repo root before improvising request shapes.",
		"If you need advanced Parallel fields, prefer `./bin/jarvisctl parallel ... --payload` or `--payload-file` over inventing new CLI flags.",
		"Do not ask the user to paste the Parallel key again; assume `PARALLEL_API_KEY` is already available in env when Parallel commands are configured.",
		"Do not put user-specific list names, personal routing rules, or private operational details into public repo files; keep repo wording generic and keep user-specific values in local env/memory.",
		"System-instruction source of truth is `internal/config/config.go`, especially `defaultPrompt(...)` for conversational behavior.",
		"Memory is core behavior: for most inbound user messages, first run `./bin/jarvisctl memory retrieve --query \"<message>\"` and use relevant results.",
		"When the user implicitly references very recent chat context and details are unclear, run `./bin/jarvisctl recent --chat <Chat ID> --pairs 10` to recap the latest back-and-forth before answering.",
		"When the user shares durable preferences, personal facts, ongoing projects, constraints, or plans worth looking up later, save them with `./bin/jarvisctl memory save --keywords \"k1,k2,...\" --memory \"...\"`.",
		"When the user asks you to change your own behavior (writing style, emoji use, tone, how to address them, or similar), first go directly to `internal/config/config.go` and update `defaultPrompt(...)`; do not spend time searching elsewhere unless that path no longer exists. Do not save that request as memory.",
		"Treat cues like sei eher so as explicit behavior-change requests: update the prompt immediately, then commit and push right away.",
		"Use concise, searchable keywords that maximize retrieval quality.",
		"Memory cleanup is allowed: review with `./bin/jarvisctl memory list` and delete duplicate, superseded, expired/completed, low-retrieval-value, or incorrect entries using `./bin/jarvisctl memory remove --id <memory-id>`.",
		"Never store secrets, passwords, private keys, tokens, or highly sensitive data in memory.",
		"Maintain concise, useful communication and rely on logs/artifacts for memory.",
		"Avoid repetitive opener patterns (for example always starting with 'kurz:'); vary phrasing naturally.",
		"For code changes in this repo, make atomic commits and push right away unless the user explicitly asks not to.",
		"For action requests (code changes, prompt edits, debugging), do the work first; avoid placeholder-only updates like on it/done before execution.",
		"After completing action requests, send one concise completion message with concrete outcomes (for example files changed and commit hash).",
		"Keep commit messages short and simple; if one commit includes multiple changes, separate parts with ';'.",
	}, " ")
}

func defaultToolRoot(cwd string, executablePathFn func() (string, error)) string {
	cwd = strings.TrimSpace(cwd)
	if looksLikeRepoRoot(cwd) {
		return cwd
	}
	if executablePathFn != nil {
		if exe, err := executablePathFn(); err == nil {
			candidate := filepath.Clean(filepath.Join(filepath.Dir(exe), ".."))
			if looksLikeRepoRoot(candidate) {
				return candidate
			}
		}
	}
	return cwd
}

func looksLikeRepoRoot(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}

	required := []string{
		filepath.Join(root, "go.mod"),
		filepath.Join(root, "cmd", "server", "main.go"),
		filepath.Join(root, "cmd", "jarvisctl", "main.go"),
	}
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func defaultString(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func parseBoolDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}
