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
	heartbeatPrompt := strings.TrimSpace(os.Getenv("JARVIS_PHI_HEARTBEAT_PROMPT"))
	if heartbeatPrompt == "" {
		heartbeatPrompt = "Heartbeat check-in: review recent context, decide whether to message, and if messaging is useful send explicitly via jarvisctl telegram command(s)."
	}

	thinking := parseThinkingLevel(os.Getenv("JARVIS_PHI_THINKING"))
	openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	elevenLabsKey := strings.TrimSpace(os.Getenv("ELEVENLABS_API_KEY"))

	cfg := Config{
		Env:                  defaultString("JARVIS_PHI_ENV", "dev"),
		ListenAddr:           defaultString("JARVIS_PHI_LISTEN_ADDR", ":8080"),
		DataDir:              dataDir,
		TelegramBotToken:     strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramWebhookToken: strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
		TelegramAPIBase:      defaultString("JARVIS_PHI_TELEGRAM_API_BASE", "https://api.telegram.org"),
		Timezone:             defaultString("JARVIS_PHI_TZ", "UTC"),
		PhiAuthMode:          authMode,
		PhiModelID:           modelID,
		PhiThinking:          thinking,
		PhiToolRoot:          toolRoot,
		PhiSystemPrompt:      defaultPrompt(),
		PhiAPIKey:            openAIKey,
		PhiAccessToken:       strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCESS_TOKEN")),
		PhiAccountID:         strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCOUNT_ID")),
		DefaultChatID:        defaultChatID,
		OpenAIAPIKey:         openAIKey,
		TranscriptionEnabled: parseBoolDefault("JARVIS_PHI_TRANSCRIPTION_ENABLED", openAIKey != ""),
		ElevenLabsAPIKey:     elevenLabsKey,
		ElevenLabsVoiceID:    defaultString("ELEVENLABS_VOICE_ID", "EkK5I93UQWFDigLMpZcX"),
		VoiceReplyEnabled:    parseBoolDefault("JARVIS_PHI_VOICE_REPLY_ENABLED", elevenLabsKey != ""),
		HeartbeatEnabled:     heartbeatEnabled,
		HeartbeatPrompt:      heartbeatPrompt,
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

func defaultPrompt() string {
	return strings.Join([]string{
		"You are Jarvis running inside a Telegram wrapper on top of phi.",
		"Always use CLI-first workflows via bash when taking actions.",
		"To send anything to Telegram, explicitly call `jarvisctl telegram ...` using bash.",
		"For each inbound Telegram message, send at least one reply via `jarvisctl telegram send-text --chat <Chat ID> --text ...` unless the user explicitly asks for silence.",
		"Do not assume your final assistant text is delivered to the user.",
		"If you do not call a telegram send command, nothing is sent.",
		"Use --help when exploring unfamiliar CLI commands.",
		"When scheduling tasks, use `jarvisctl schedule ...` and keep schedules precise.",
		"Maintain concise, useful communication and rely on logs/artifacts for memory.",
	}, " ")
}

func parseThinkingLevel(raw string) agent.ThinkingLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off":
		return agent.ThinkingOff
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
