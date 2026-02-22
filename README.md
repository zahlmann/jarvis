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
- ask how Jarvis should address you (`JARVIS_USER_NAME`)
- ask for required OpenAI API setup (used for memory embeddings + voice transcription)
- ask optional voice-reply setup (ElevenLabs key)
- auto-write `.env`
- build the server binary
- install reboot autostart + crash restart via a background supervisor
- start Jarvis immediately
- guide Cloudflare Zero Trust tunnel setup and optionally auto-register Telegram webhook

When `wake-jarvis.sh` asks for the public URL, use this Cloudflare Zero Trust flow:

1. Open Zero Trust dashboard.
2. Go to `Network` -> `Connectors`.
3. Click `Create Tunnel`.
4. Add a public hostname that forwards to your local Jarvis listener (default `http://127.0.0.1:8080`).
5. Copy the `https://...` URL and paste it into `wake-jarvis.sh`; the script will run `setWebhook` for you.

## What it does

- Telegram webhook ingress (`/telegram/webhook`)
- `phi`-backed agent runtime (ChatGPT subscription mode via `PHI_AUTH_MODE=chatgpt`)
- Full `phi` coding tools enabled (`write/read/edit/bash`)
- Explicit-send contract: agent must call `./bin/jarvisctl telegram ...` from repo root; final assistant text is not auto-delivered
- Persistent memory in parquet (`jarvisctl memory save|retrieve|list|remove`) with background embedding backfill every minute
- Internal scheduler with persistent jobs + internal `heartbeat`
- Heartbeat policy: every 30 minutes with jitter `-10m..+10m`; skipped if busy through window
- Default [Bring app](https://www.getbring.com/) integration (`jarvisctl bring list|add|remove|complete`)
- Structured JSONL logs for inbound, stream/tool events, scheduler decisions, and outbound CLI sends

## Env

If you already completed the interactive `./wake-jarvis.sh` setup, manual env setup is usually not needed because the script writes `.env` for you.

For manual setup, copy `.env.example` to `.env` and set at least:

- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_WEBHOOK_SECRET`
- `JARVIS_USER_NAME`
- `PHI_AUTH_MODE=chatgpt`
- `PHI_CHATGPT_ACCESS_TOKEN` (or preexisting `~/.phi/chatgpt_tokens.json`)
- `OPENAI_API_KEY` (required for memory embeddings + voice transcription)
- `JARVIS_PHI_DEFAULT_CHAT_ID` (required for heartbeat routing)

Feature toggles:

- `JARVIS_PHI_TRANSCRIPTION_ENABLED` controls inbound voice transcription handling (default `true`)
- `JARVIS_PHI_VOICE_REPLY_ENABLED` controls `jarvisctl telegram send-voice`
- `JARVIS_PHI_THINKING` controls model reasoning effort (`none|minimal|low|medium|high|xhigh`, default `xhigh`)
- `JARVIS_PHI_MEMORY_EMBEDDING_MODEL` controls the embedding model for memory retrieval (default `text-embedding-3-small`)

If using voice:

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

Agent runtime canonical command form (used by `phi` tool execution):

```bash
cd /path/to/jarvis
./bin/jarvisctl telegram send-text --chat 123456 --text "hello"
```

Use exact supported Telegram CLI flags/subcommands. Variants like `telegram send` or `--chat-id` are invalid.

Webhook setup helper:

```bash
go run ./cmd/jarvisctl telegram set-webhook --url https://YOUR_DOMAIN/telegram/webhook
```

`set-webhook` uses `TELEGRAM_WEBHOOK_SECRET` from `.env` by default, so you do not need to paste the token manually again.

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

# Save memory
go run ./cmd/jarvisctl memory save --keywords "coffee,morning" --memory "user prefers black coffee in the morning"

# Retrieve related memories
go run ./cmd/jarvisctl memory retrieve --query "what coffee does the user like?" --limit 5
```

## Data layout

- `data/logs/events-YYYY-MM-DD.jsonl`
- `data/messages/dedup.json`
- `data/messages/index.json`
- `data/memory/memories.parquet`
- `data/sessions/chat-<id>.jsonl`
- `data/scheduler/jobs.json`
- `data/heartbeat/state.json`

## Tests

If running in a sandboxed/CI/container environment, set the same env vars from the Commands section first.

```bash
go test ./...
```
