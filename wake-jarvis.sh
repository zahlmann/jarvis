#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$ROOT_DIR/.env"
ENV_EXAMPLE_FILE="$ROOT_DIR/.env.example"
BIN_DIR="$ROOT_DIR/bin"
BIN_PATH="$BIN_DIR/jarvis-phi-server"
JARVISCTL_BIN_PATH="$BIN_DIR/jarvisctl"
SCRIPT_DIR="$ROOT_DIR/scripts"
SUPERVISOR_PATH="$SCRIPT_DIR/jarvis-supervisor.sh"
LOG_DIR="$ROOT_DIR/data/logs"
AUTOSTART_TAG="# jarvis-phi-autostart"

export GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
export GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}"
export GOPATH="${GOPATH:-$ROOT_DIR/.cache/go}"

mkdir -p "$BIN_DIR" "$SCRIPT_DIR" "$LOG_DIR" "$GOCACHE" "$GOMODCACHE" "$GOPATH"

if [[ ! -t 0 ]]; then
  printf "wake-jarvis.sh requires an interactive terminal.\n" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  printf "Go is required but not found in PATH. Install Go first, then rerun this script.\n" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  if [[ -f "$ENV_EXAMPLE_FILE" ]]; then
    cp "$ENV_EXAMPLE_FILE" "$ENV_FILE"
  else
    touch "$ENV_FILE"
  fi
fi

trim() {
  local s="$1"
  s="${s#${s%%[![:space:]]*}}"
  s="${s%${s##*[![:space:]]}}"
  printf "%s" "$s"
}

get_env_value() {
  local key="$1"
  local line
  line="$(grep -E "^${key}=" "$ENV_FILE" | tail -n 1 || true)"
  if [[ -z "$line" ]]; then
    printf ""
    return
  fi
  local value="${line#*=}"
  value="$(trim "$value")"
  if [[ "$value" == \"*\" && "$value" == *\" ]]; then
    value="${value:1:${#value}-2}"
  fi
  printf "%s" "$value"
}

upsert_env() {
  local key="$1"
  local value="$2"
  local tmp_file
  tmp_file="$(mktemp)"

  if grep -qE "^${key}=" "$ENV_FILE"; then
    awk -v k="$key" -v v="$value" '
      BEGIN { updated=0 }
      $0 ~ "^"k"=" {
        if (updated == 0) {
          print k"="v
          updated=1
        }
        next
      }
      { print }
      END {
        if (updated == 0) {
          print k"="v
        }
      }
    ' "$ENV_FILE" > "$tmp_file"
  else
    cat "$ENV_FILE" > "$tmp_file"
    printf "%s=%s\n" "$key" "$value" >> "$tmp_file"
  fi

  mv "$tmp_file" "$ENV_FILE"
}

prompt_value() {
  local key="$1"
  local prompt="$2"
  local secret="${3:-false}"
  local current
  current="$(get_env_value "$key")"

  if [[ -n "$current" ]]; then
    printf "%s already set for %s.\n" "$key" "$key"
    return
  fi

  local input=""
  while [[ -z "$(trim "$input")" ]]; do
    if [[ "$secret" == "true" ]]; then
      read -r -s -p "$prompt: " input
      printf "\n"
    else
      read -r -p "$prompt: " input
    fi
    input="$(trim "$input")"
  done

  upsert_env "$key" "$input"
}

ask_yes_no() {
  local prompt="$1"
  local default="$2"
  local answer=""
  while true; do
    if [[ "$default" == "y" ]]; then
      read -r -p "$prompt [Y/n]: " answer
      answer="${answer:-y}"
    else
      read -r -p "$prompt [y/N]: " answer
      answer="${answer:-n}"
    fi
    answer="$(echo "$answer" | tr '[:upper:]' '[:lower:]' | xargs)"
    if [[ "$answer" == "y" || "$answer" == "yes" ]]; then
      return 0
    fi
    if [[ "$answer" == "n" || "$answer" == "no" ]]; then
      return 1
    fi
    printf "Please answer y or n.\n"
  done
}

generate_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    date +%s%N
  fi
}

local_target_url() {
  local listen_addr="$1"
  local target
  target="$(trim "$listen_addr")"
  if [[ -z "$target" ]]; then
    target=":8080"
  fi
  if [[ "$target" == :* ]]; then
    target="127.0.0.1$target"
  fi
  if [[ "$target" == 0.0.0.0:* ]]; then
    target="127.0.0.1:${target##*:}"
  fi
  if [[ "$target" != http://* && "$target" != https://* ]]; then
    target="http://$target"
  fi
  printf "%s" "$target"
}

printf "== Jarvis bootstrap ==\n"
printf "Repository: %s\n" "$ROOT_DIR"

upsert_env "PHI_AUTH_MODE" "chatgpt"

prompt_value "TELEGRAM_BOT_TOKEN" "Telegram Bot Token" true

webhook_secret="$(get_env_value "TELEGRAM_WEBHOOK_SECRET")"
if [[ -z "$webhook_secret" ]]; then
  generated="$(generate_secret)"
  read -r -p "Telegram webhook secret token (press Enter to auto-generate): " webhook_secret
  webhook_secret="$(trim "$webhook_secret")"
  if [[ -z "$webhook_secret" ]]; then
    webhook_secret="$generated"
    printf "Generated webhook secret token.\n"
  fi
  upsert_env "TELEGRAM_WEBHOOK_SECRET" "$webhook_secret"
fi

chatgpt_token="$(get_env_value "PHI_CHATGPT_ACCESS_TOKEN")"
chatgpt_store="$HOME/.phi/chatgpt_tokens.json"
if [[ -z "$chatgpt_token" && ! -f "$chatgpt_store" ]]; then
  prompt_value "PHI_CHATGPT_ACCESS_TOKEN" "ChatGPT access token (required if ~/.phi/chatgpt_tokens.json is missing)" true
elif [[ -z "$chatgpt_token" && -f "$chatgpt_store" ]]; then
  printf "Found existing ChatGPT tokens at %s, no token prompt needed.\n" "$chatgpt_store"
fi

openai_key="$(get_env_value "OPENAI_API_KEY")"
if [[ -n "$openai_key" ]]; then
  printf "OPENAI_API_KEY already configured, voice transcription will stay enabled.\n"
  upsert_env "JARVIS_PHI_TRANSCRIPTION_ENABLED" "true"
else
  if ask_yes_no "do you want jarvis to be able to understand your voice messages? (OpenAI API Key needed)" "n"; then
    prompt_value "OPENAI_API_KEY" "OpenAI API Key" true
    upsert_env "JARVIS_PHI_TRANSCRIPTION_ENABLED" "true"
  else
    upsert_env "OPENAI_API_KEY" ""
    upsert_env "JARVIS_PHI_TRANSCRIPTION_ENABLED" "false"
  fi
fi

if ask_yes_no "do you want jarvis to be able to talk to you via voice messages? (ElevenLabs API Key needed)" "n"; then
  prompt_value "ELEVENLABS_API_KEY" "ElevenLabs API Key" true
  if [[ -z "$(get_env_value "ELEVENLABS_VOICE_ID")" ]]; then
    upsert_env "ELEVENLABS_VOICE_ID" "EkK5I93UQWFDigLMpZcX"
  fi
  upsert_env "JARVIS_PHI_VOICE_REPLY_ENABLED" "true"
else
  upsert_env "ELEVENLABS_API_KEY" ""
  upsert_env "JARVIS_PHI_VOICE_REPLY_ENABLED" "false"
fi

if [[ -z "$(get_env_value "JARVIS_PHI_DEFAULT_CHAT_ID")" ]]; then
  read -r -p "Your Telegram chat id for heartbeat reminders (optional, press Enter to skip): " default_chat_id
  default_chat_id="$(trim "$default_chat_id")"
  if [[ -n "$default_chat_id" ]]; then
    upsert_env "JARVIS_PHI_DEFAULT_CHAT_ID" "$default_chat_id"
    upsert_env "JARVIS_PHI_HEARTBEAT_ENABLED" "true"
  else
    upsert_env "JARVIS_PHI_HEARTBEAT_ENABLED" "false"
  fi
fi

if ask_yes_no "Do you want to set up Bring shopping list integration now?" "n"; then
  prompt_value "BRING_EMAIL" "Bring account email"
  prompt_value "BRING_PASSWORD" "Bring account password" true
fi

if [[ -z "$(get_env_value "JARVIS_PHI_LISTEN_ADDR")" ]]; then
  upsert_env "JARVIS_PHI_LISTEN_ADDR" ":8080"
fi
if [[ -z "$(get_env_value "JARVIS_PHI_TZ")" ]]; then
  upsert_env "JARVIS_PHI_TZ" "Europe/Vienna"
fi
if [[ -z "$(get_env_value "JARVIS_PHI_DATA_DIR")" ]]; then
  upsert_env "JARVIS_PHI_DATA_DIR" "./data"
fi
if [[ -z "$(get_env_value "JARVIS_PHI_TOOL_ROOT")" ]]; then
  upsert_env "JARVIS_PHI_TOOL_ROOT" "./"
fi
if [[ -z "$(get_env_value "JARVIS_PHI_MODEL")" ]]; then
  upsert_env "JARVIS_PHI_MODEL" "gpt-5.3-codex"
fi

printf "Building jarvis binaries...\n"
(
  cd "$ROOT_DIR"
  go build -o "$BIN_PATH" ./cmd/server
  go build -o "$JARVISCTL_BIN_PATH" ./cmd/jarvisctl
)

cat > "$SUPERVISOR_PATH" <<SUPERVISOR
#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$ROOT_DIR"
cd "\$ROOT_DIR"
export PATH="\$ROOT_DIR/bin:\$PATH"
mkdir -p "\$ROOT_DIR/data/logs"
while true; do
  "\$ROOT_DIR/bin/jarvis-phi-server" >> "\$ROOT_DIR/data/logs/server.out.log" 2>&1
  sleep 3
done
SUPERVISOR
chmod +x "$SUPERVISOR_PATH"

if command -v crontab >/dev/null 2>&1; then
  autostart_line="@reboot /bin/bash -lc \"nohup $SUPERVISOR_PATH >/dev/null 2>&1 &\" $AUTOSTART_TAG"
  existing_cron="$(crontab -l 2>/dev/null || true)"
  if ! printf "%s\n" "$existing_cron" | grep -Fq "$AUTOSTART_TAG"; then
    tmp_cron="$(mktemp)"
    if [[ -n "$existing_cron" ]]; then
      printf "%s\n" "$existing_cron" > "$tmp_cron"
    fi
    printf "%s\n" "$autostart_line" >> "$tmp_cron"
    crontab "$tmp_cron"
    rm -f "$tmp_cron"
    printf "Installed reboot autostart via crontab.\n"
  else
    printf "Reboot autostart already installed in crontab.\n"
  fi
else
  printf "crontab is not available; skipping reboot autostart installation.\n"
fi

if pgrep -f "$SUPERVISOR_PATH" >/dev/null 2>&1; then
  printf "Jarvis supervisor already running.\n"
else
  nohup "$SUPERVISOR_PATH" >/dev/null 2>&1 &
  sleep 1
  printf "Jarvis started in background.\n"
fi

listen_addr="$(get_env_value "JARVIS_PHI_LISTEN_ADDR")"
listen_addr="${listen_addr:-:8080}"
local_target="$(local_target_url "$listen_addr")"

printf "\nWebhook setup:\n"
printf "Telegram needs a public HTTPS endpoint for /telegram/webhook.\n"
printf "Jarvis local listener: %s\n" "$listen_addr"
printf "If this machine is not publicly reachable, create a tunnel first.\n"
printf "Cloudflare Zero Trust flow:\n"
printf "  1) Open Zero Trust dashboard.\n"
printf "  2) Go to Network -> Connectors.\n"
printf "  3) Create Tunnel.\n"
printf "  4) Add a public hostname that forwards to %s.\n" "$local_target"
printf "  5) Copy the public https://... URL and paste it below.\n"

read -r -p "Public HTTPS base URL (press Enter to skip for now): " webhook_base_url
webhook_base_url="$(trim "$webhook_base_url")"
webhook_configured="false"
webhook_url=""
if [[ -n "$webhook_base_url" ]]; then
  webhook_base_url="${webhook_base_url%/}"
  webhook_url="$webhook_base_url/telegram/webhook"
  printf "Using TELEGRAM_WEBHOOK_SECRET from .env as Telegram secret token.\n"
  printf "Registering Telegram webhook at %s...\n" "$webhook_url"
  if (
    cd "$ROOT_DIR"
    go run ./cmd/jarvisctl telegram set-webhook --url "$webhook_url"
  ); then
    webhook_configured="true"
    printf "Telegram webhook configured successfully.\n"
  else
    printf "Webhook registration failed. You can retry later with:\n"
    printf "  go run ./cmd/jarvisctl telegram set-webhook --url %s\n" "$webhook_url"
  fi
fi

printf "\nDone.\n"
printf -- "- Env file: %s\n" "$ENV_FILE"
printf -- "- Log file: %s\n" "$LOG_DIR/server.out.log"
printf -- "- Background supervisor: %s\n" "$SUPERVISOR_PATH"
printf "\nNext:\n"
if [[ "$webhook_configured" == "true" ]]; then
  printf "1) Webhook active: %s\n" "$webhook_url"
  printf "2) Watch logs: tail -f %s\n" "$LOG_DIR/server.out.log"
else
  printf "1) Expose %s on a public HTTPS URL (Zero Trust steps shown above).\n" "$local_target"
  printf "2) Run: go run ./cmd/jarvisctl telegram set-webhook --url https://YOUR_DOMAIN/telegram/webhook\n"
  printf "3) Watch logs: tail -f %s\n" "$LOG_DIR/server.out.log"
fi
