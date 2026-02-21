package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/bring"
	"github.com/zahlmann/jarvis-phi/internal/cli"
	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/media"
	"github.com/zahlmann/jarvis-phi/internal/scheduler"
	"github.com/zahlmann/jarvis-phi/internal/store"
	"github.com/zahlmann/jarvis-phi/internal/telegram"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "telegram":
		handleTelegram(os.Args[2:])
	case "schedule":
		handleSchedule(os.Args[2:])
	case "bring":
		handleBring(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`jarvisctl commands:
  telegram send-text --chat <id> --text <msg>
  telegram send-voice --chat <id> --text <msg>
  telegram send-audio-file --chat <id> --path <file>
  telegram send-photo --chat <id> --path <file> [--caption <text>]
  schedule add|update|remove|list|run-due
  bring list|add|remove|complete ...`)
}

func handleTelegram(args []string) {
	if len(args) < 1 {
		cli.Exitf("telegram subcommand required")
	}
	cfg, err := config.LoadWithOptions(config.LoadOptions{RequireTelegramToken: true})
	if err != nil {
		cli.Exitf("config error: %v", err)
	}
	logger, err := logstore.New(filepath.Join(cfg.DataDir, "logs"))
	if err != nil {
		cli.Exitf("log store error: %v", err)
	}
	index, err := store.NewMessageIndex(filepath.Join(cfg.DataDir, "messages", "index.json"))
	if err != nil {
		cli.Exitf("message index error: %v", err)
	}
	client := telegram.NewClient(cfg.TelegramBotToken, cfg.TelegramAPIBase)

	sub := args[0]
	switch sub {
	case "send-text":
		fs := flag.NewFlagSet("send-text", flag.ExitOnError)
		chatID := fs.Int64("chat", 0, "chat id")
		text := fs.String("text", "", "text")
		_ = fs.Parse(args[1:])
		if *chatID == 0 {
			cli.Exitf("--chat is required")
		}
		res, err := client.SendText(*chatID, *text)
		if err != nil {
			cli.Exitf("send-text failed: %v", err)
		}
		rec := store.MessageRecord{ChatID: *chatID, MessageID: res.MessageID, Direction: "outbound", Sender: "jarvis", Text: *text}
		_ = index.Put(rec)
		_ = logger.Write("telegram", "send_text", map[string]any{"chat_id": *chatID, "message_id": res.MessageID, "chars": len(*text)})
		cli.PrintJSON(map[string]any{"ok": true, "message_id": res.MessageID})
	case "send-voice":
		fs := flag.NewFlagSet("send-voice", flag.ExitOnError)
		chatID := fs.Int64("chat", 0, "chat id")
		text := fs.String("text", "", "tts text")
		_ = fs.Parse(args[1:])
		if !cfg.VoiceReplyEnabled {
			cli.Exitf("voice replies are disabled (set JARVIS_PHI_VOICE_REPLY_ENABLED=true and ELEVENLABS_API_KEY)")
		}
		if *chatID == 0 {
			cli.Exitf("--chat is required")
		}
		if strings.TrimSpace(*text) == "" {
			cli.Exitf("--text is required")
		}
		audio, err := media.TextToSpeech(context.Background(), cfg.ElevenLabsAPIKey, cfg.ElevenLabsVoiceID, *text)
		if err != nil {
			cli.Exitf("tts failed: %v", err)
		}
		tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("jarvis-phi-tts-%d.mp3", time.Now().UnixNano()))
		if writeErr := os.WriteFile(tempPath, audio, 0o600); writeErr != nil {
			cli.Exitf("write temp audio failed: %v", writeErr)
		}
		defer os.Remove(tempPath)

		res, err := client.SendAudioFile(*chatID, tempPath, "")
		if err != nil {
			cli.Exitf("send-voice failed: %v", err)
		}
		_ = index.Put(store.MessageRecord{ChatID: *chatID, MessageID: res.MessageID, Direction: "outbound", Sender: "jarvis", Text: "[voice] " + *text})
		_ = logger.Write("telegram", "send_voice", map[string]any{"chat_id": *chatID, "message_id": res.MessageID, "chars": len(*text)})
		cli.PrintJSON(map[string]any{"ok": true, "message_id": res.MessageID})
	case "send-audio-file":
		fs := flag.NewFlagSet("send-audio-file", flag.ExitOnError)
		chatID := fs.Int64("chat", 0, "chat id")
		path := fs.String("path", "", "audio file path")
		caption := fs.String("caption", "", "optional caption")
		_ = fs.Parse(args[1:])
		if *chatID == 0 || strings.TrimSpace(*path) == "" {
			cli.Exitf("--chat and --path are required")
		}
		res, err := client.SendAudioFile(*chatID, *path, *caption)
		if err != nil {
			cli.Exitf("send-audio-file failed: %v", err)
		}
		_ = index.Put(store.MessageRecord{ChatID: *chatID, MessageID: res.MessageID, Direction: "outbound", Sender: "jarvis", Text: "[audio-file] " + filepath.Base(*path)})
		_ = logger.Write("telegram", "send_audio_file", map[string]any{"chat_id": *chatID, "message_id": res.MessageID, "path": *path})
		cli.PrintJSON(map[string]any{"ok": true, "message_id": res.MessageID})
	case "send-photo":
		fs := flag.NewFlagSet("send-photo", flag.ExitOnError)
		chatID := fs.Int64("chat", 0, "chat id")
		path := fs.String("path", "", "photo path")
		caption := fs.String("caption", "", "caption")
		_ = fs.Parse(args[1:])
		if *chatID == 0 || strings.TrimSpace(*path) == "" {
			cli.Exitf("--chat and --path are required")
		}
		res, err := client.SendPhotoFile(*chatID, *path, *caption)
		if err != nil {
			cli.Exitf("send-photo failed: %v", err)
		}
		_ = index.Put(store.MessageRecord{ChatID: *chatID, MessageID: res.MessageID, Direction: "outbound", Sender: "jarvis", Text: "[photo] " + *caption})
		_ = logger.Write("telegram", "send_photo", map[string]any{"chat_id": *chatID, "message_id": res.MessageID, "path": *path})
		cli.PrintJSON(map[string]any{"ok": true, "message_id": res.MessageID})
	default:
		cli.Exitf("unknown telegram command: %s", sub)
	}
}

func handleSchedule(args []string) {
	if len(args) < 1 {
		cli.Exitf("schedule subcommand required")
	}
	cfg, err := config.LoadWithOptions(config.LoadOptions{})
	if err != nil {
		cli.Exitf("config error: %v", err)
	}
	logger, err := logstore.New(filepath.Join(cfg.DataDir, "logs"))
	if err != nil {
		cli.Exitf("log store error: %v", err)
	}
	st, err := scheduler.NewStore(filepath.Join(cfg.DataDir, "scheduler", "jobs.json"))
	if err != nil {
		cli.Exitf("scheduler store error: %v", err)
	}

	now := time.Now().UTC()
	sub := args[0]
	switch sub {
	case "add", "update":
		fs := flag.NewFlagSet(sub, flag.ExitOnError)
		id := fs.String("id", "", "job id")
		chatID := fs.Int64("chat", 0, "chat id")
		prompt := fs.String("prompt", "", "prompt")
		mode := fs.String("mode", "", "once|cron|interval")
		cronExpr := fs.String("cron", "", "cron expression")
		runAt := fs.String("run-at", "", "RFC3339 timestamp")
		interval := fs.String("interval", "", "duration (e.g. 30m)")
		tz := fs.String("tz", "", "IANA timezone")
		disabled := fs.Bool("disabled", false, "set enabled=false")
		_ = fs.Parse(args[1:])

		job := scheduler.Job{
			ID:       strings.TrimSpace(*id),
			Kind:     scheduler.KindUser,
			ChatID:   *chatID,
			Prompt:   strings.TrimSpace(*prompt),
			Mode:     scheduler.JobMode(strings.TrimSpace(*mode)),
			CronExpr: strings.TrimSpace(*cronExpr),
			RunAt:    strings.TrimSpace(*runAt),
			Interval: strings.TrimSpace(*interval),
			Timezone: strings.TrimSpace(*tz),
			Enabled:  !*disabled,
		}
		saved, err := st.Upsert(job, now, cfg.Timezone)
		if err != nil {
			cli.Exitf("schedule %s failed: %v", sub, err)
		}
		_ = logger.Write("schedule_cli", sub, map[string]any{"job_id": saved.ID, "chat_id": saved.ChatID})
		cli.PrintJSON(saved)
	case "remove":
		fs := flag.NewFlagSet("remove", flag.ExitOnError)
		id := fs.String("id", "", "job id")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*id) == "" {
			cli.Exitf("--id is required")
		}
		removed, err := st.Remove(*id)
		if err != nil {
			cli.Exitf("remove failed: %v", err)
		}
		_ = logger.Write("schedule_cli", "remove", map[string]any{"job_id": *id, "removed": removed})
		cli.PrintJSON(map[string]any{"ok": true, "removed": removed})
	case "list":
		jobs, err := st.List()
		if err != nil {
			cli.Exitf("list failed: %v", err)
		}
		cli.PrintJSON(map[string]any{"jobs": jobs})
	case "run-due":
		fs := flag.NewFlagSet("run-due", flag.ExitOnError)
		atRaw := fs.String("at", "", "optional RFC3339 timestamp")
		_ = fs.Parse(args[1:])
		runAt := now
		if strings.TrimSpace(*atRaw) != "" {
			parsed, parseErr := time.Parse(time.RFC3339, *atRaw)
			if parseErr != nil {
				cli.Exitf("invalid --at: %v", parseErr)
			}
			runAt = parsed.UTC()
		}
		due, err := st.Due(runAt)
		if err != nil {
			cli.Exitf("run-due failed: %v", err)
		}
		cli.PrintJSON(map[string]any{"at": runAt.Format(time.RFC3339), "due": due})
	default:
		cli.Exitf("unknown schedule command: %s", sub)
	}
}

func handleBring(args []string) {
	if len(args) < 1 {
		cli.Exitf("bring subcommand required")
	}
	output, err := bring.Run(args)
	if err != nil {
		cli.Exitf("bring failed: %v", err)
	}
	fmt.Println(output)
}
