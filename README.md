# jarvis

Go-native Telegram wrapper around [`phi`](https://github.com/zahlmann/phi).

## Quick Start

```bash
git clone <your-repo-url> jarvis
cd jarvis
./wake-jarvis.sh
```

`wake-jarvis.sh` will:

- ask for required Telegram + ChatGPT auth setup
- ask optional voice-transcription setup (OpenAI key)
- ask optional voice-reply setup (ElevenLabs key)
- auto-write `.env`
- build the server binary
- install reboot autostart + crash restart via a background supervisor
- start Jarvis immediately

## What it does

- Telegram webhook ingress (`/telegram/webhook`)
- `phi`-backed agent runtime (ChatGPT subscription mode via `PHI_AUTH_MODE=chatgpt`)
- Full `phi` coding tools enabled (`write/read/edit/bash`)
- Explicit-send contract: agent must call `jarvisctl telegram ...`; final assistant text is not auto-delivered
- Internal scheduler with persistent jobs + internal `heartbeat`
- Heartbeat policy: every 30 minutes with jitter `-10m..+10m`; skipped if busy through window
- Default [Bring app](https://www.getbring.com/) integration (`jarvisctl bring list|add|remove|complete`)
- Structured JSONL logs for inbound, stream/tool events, scheduler decisions, and outbound CLI sends

## Env

If you already completed the interactive `./wake-jarvis.sh` setup, manual env setup is usually not needed because the script writes `.env` for you.

For manual setup, copy `.env.example` to `.env` and set at least:

- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_WEBHOOK_SECRET`
- `PHI_AUTH_MODE=chatgpt`
- `PHI_CHATGPT_ACCESS_TOKEN` (or preexisting `~/.phi/chatgpt_tokens.json`)
- `JARVIS_PHI_DEFAULT_CHAT_ID` (required for heartbeat routing)

Feature toggles:

- `JARVIS_PHI_TRANSCRIPTION_ENABLED` controls inbound voice transcription handling
- `JARVIS_PHI_VOICE_REPLY_ENABLED` controls `jarvisctl telegram send-voice`

If using voice:

- `OPENAI_API_KEY` (voice transcription)
- `ELEVENLABS_API_KEY` (+ optional `ELEVENLABS_VOICE_ID`)

If using the Bring app:

- `BRING_EMAIL`
- `BRING_PASSWORD`
- optional `BRING_LIST_UUID` or `BRING_LIST_NAME`

## Commands

If running in a sandboxed/CI/container environment, set:

```bash
export GOCACHE=/tmp/go-build-cache
export GOMODCACHE=/tmp/go-mod-cache
export GOPATH=/tmp/go
```

Run server:

```bash
go run ./cmd/server
```

Run CLI:

```bash
go run ./cmd/jarvisctl --help
```

Examples:

```bash
# Send message
go run ./cmd/jarvisctl telegram send-text --chat 123456 --text "hello"

# Add scheduled prompt
go run ./cmd/jarvisctl schedule add \
  --id morning-check --chat 123456 --prompt "do a quick check-in" --mode cron --cron "0 9 * * *" --tz Europe/Vienna

# List schedules
go run ./cmd/jarvisctl schedule list

# Bring list and add
go run ./cmd/jarvisctl bring list
go run ./cmd/jarvisctl bring add "Milk" "Eggs" "Butter:unsalted"
```

## Data layout

- `data/logs/events-YYYY-MM-DD.jsonl`
- `data/messages/dedup.json`
- `data/messages/index.json`
- `data/sessions/chat-<id>.jsonl`
- `data/scheduler/jobs.json`
- `data/heartbeat/state.json`

## Tests

If running in a sandboxed/CI/container environment, set the same env vars from the Commands section first.

```bash
go test ./...
```
