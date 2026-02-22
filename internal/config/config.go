package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zahlmann/phi/agent"
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
	PhiThinking     agent.ThinkingLevel
	PhiToolRoot     string
	PhiSystemPrompt string

	PhiAPIKey      string
	PhiAccessToken string
	PhiAccountID   string

	DefaultChatID int64

	OpenAIAPIKey         string
	MemoryEmbeddingModel string
	TranscriptionEnabled bool
	ElevenLabsAPIKey     string
	ElevenLabsVoiceID    string
	VoiceReplyEnabled    bool

	HeartbeatEnabled bool
	HeartbeatPrompt  string
}

type LoadOptions struct {
	RequireTelegramToken  bool
	RequirePhiCredentials bool
}

func Load() (Config, error) {
	return LoadWithOptions(LoadOptions{
		RequireTelegramToken:  true,
		RequirePhiCredentials: true,
	})
}

func LoadWithOptions(opts LoadOptions) (Config, error) {
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
		toolRoot = cwd
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

	heartbeatEnabled := parseBoolDefault("JARVIS_PHI_HEARTBEAT_ENABLED", true)
	heartbeatPrompt := defaultHeartbeatPrompt()

	thinking := parseThinkingLevel(os.Getenv("JARVIS_PHI_THINKING"))
	openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	elevenLabsKey := strings.TrimSpace(os.Getenv("ELEVENLABS_API_KEY"))
	userName := strings.TrimSpace(os.Getenv("JARVIS_USER_NAME"))
	if userName == "" {
		userName = "<USER_NAME>"
	}

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
		PhiThinking:          thinking,
		PhiToolRoot:          toolRoot,
		PhiSystemPrompt:      defaultPrompt(userName),
		PhiAPIKey:            openAIKey,
		PhiAccessToken:       strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCESS_TOKEN")),
		PhiAccountID:         strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCOUNT_ID")),
		DefaultChatID:        defaultChatID,
		OpenAIAPIKey:         openAIKey,
		MemoryEmbeddingModel: defaultString("JARVIS_PHI_MEMORY_EMBEDDING_MODEL", "text-embedding-3-small"),
		TranscriptionEnabled: parseBoolDefault("JARVIS_PHI_TRANSCRIPTION_ENABLED", true),
		ElevenLabsAPIKey:     elevenLabsKey,
		ElevenLabsVoiceID:    defaultString("ELEVENLABS_VOICE_ID", "EkK5I93UQWFDigLMpZcX"),
		VoiceReplyEnabled:    parseBoolDefault("JARVIS_PHI_VOICE_REPLY_ENABLED", elevenLabsKey != ""),
		HeartbeatEnabled:     heartbeatEnabled,
		HeartbeatPrompt:      heartbeatPrompt,
	}

	if strings.TrimSpace(cfg.OpenAIAPIKey) == "" {
		return Config{}, fmt.Errorf("OPENAI_API_KEY is required")
	}

	if opts.RequireTelegramToken && cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	if opts.RequirePhiCredentials && cfg.PhiAuthMode == provider.AuthModeOpenAIAPIKey && cfg.PhiAPIKey == "" {
		return Config{}, fmt.Errorf("OPENAI_API_KEY is required for PHI_AUTH_MODE=openai_api_key")
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
		"Write like a real person texting: concise, conversational, and natural.",
		"Use lowercase in normal prose. Preserve original casing only for code, commands, paths, URLs, acronyms, and proper nouns.",
		"Say the obvious thing directly and cut through unnecessary complexity.",
		"Challenge weak ideas and overcomplicated plans instead of defaulting to agreement.",
		"Have opinions and make concrete recommendations when tradeoffs are clear.",
		"Be curious about the user and ask brief follow-up questions when context is missing.",
		"Do not get stuck repeating one topic after it was already addressed.",
		"Keep a calm tone; do not overreact to events or dates.",
		"Telegram formatting constraints: this wrapper sends plain text with sendMessage and no parse_mode; markdown/html styling is not rendered.",
		"Avoid GitHub markdown, WhatsApp markdown, and HTML tags in normal Telegram replies.",
		"If parse_mode is ever used, only Telegram Bot API formatting is valid (MarkdownV2 or HTML with Telegram-supported syntax/tags).",
		"Always use CLI-first workflows via bash when taking actions.",
		"Run CLI commands from the Jarvis home directory (repo root).",
		"To send anything to Telegram, explicitly call `./bin/jarvisctl telegram ...` using bash.",
		"For each inbound Telegram message, send at least one reply via `./bin/jarvisctl telegram send-text --chat <Chat ID> --text ...` unless the user explicitly asks for silence.",
		"Do not invent Telegram CLI variants; use exactly `send-text --chat --text` for text replies.",
		"If a send command fails, inspect stderr and retry with the exact supported command format.",
		"Do not assume your final assistant text is delivered to the user.",
		"If you do not call a telegram send command, nothing is sent.",
		"Use --help when exploring unfamiliar CLI commands.",
		"For reminders or scheduled tasks, set them up first before unrelated work.",
		"When scheduling tasks, use `./bin/jarvisctl schedule ...` and keep schedules precise.",
		"When the user mentions grocery/shopping list intent (e.g. einkaufsliste, shopping list, bring list, add/remove items on the list), use `./bin/jarvisctl bring ...` via bash.",
		"For Bring operations, use exact subcommands: `bring list`, `bring add <item...>`, `bring remove <item...>`, `bring complete <item...>`.",
		"After Bring commands, send a short Telegram confirmation with what was changed or why it failed.",
		"Memory is core behavior: for most inbound user messages, first run `./bin/jarvisctl memory retrieve --query \"<message>\"` and use relevant results.",
		"When the user shares durable preferences, personal facts, ongoing projects, constraints, or plans, save them with `./bin/jarvisctl memory save --keywords \"k1,k2,...\" --memory \"...\"`.",
		"Use concise, searchable keywords that maximize retrieval quality.",
		"Memory cleanup is allowed: review with `./bin/jarvisctl memory list` and delete stale/incorrect entries using `./bin/jarvisctl memory remove --id <memory-id>`.",
		"Never store secrets, passwords, private keys, tokens, or highly sensitive data in memory.",
		"Maintain concise, useful communication and rely on logs/artifacts for memory.",
	}, " ")
}

func defaultHeartbeatPrompt() string {
	return "Heartbeat check-in: review recent context, local time, and long-term memory. Run memory retrieval/list commands to identify stale or duplicate memories, remove low-value items, and spot anything timely worth messaging the user about. If there is anything useful, caring, or timely to say, send it now via jarvisctl telegram command(s), using short natural messages. Skip only if it is quiet hours (00:00-08:00 local) or there is truly nothing meaningful to add."
}

func parseThinkingLevel(raw string) agent.ThinkingLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return agent.ThinkingNone
	case "minimal":
		return agent.ThinkingMinimal
	case "low":
		return agent.ThinkingLow
	case "medium":
		return agent.ThinkingMedium
	case "high":
		return agent.ThinkingHigh
	case "xhigh":
		return agent.ThinkingXHigh
	default:
		return agent.ThinkingXHigh
	}
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
