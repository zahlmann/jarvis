package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zahlmann/jarvis-phi/internal/config"
	"github.com/zahlmann/jarvis-phi/internal/logstore"
	"github.com/zahlmann/jarvis-phi/internal/media"
	"github.com/zahlmann/jarvis-phi/internal/memory"
	"github.com/zahlmann/jarvis-phi/internal/runtime"
	"github.com/zahlmann/jarvis-phi/internal/scheduler"
	"github.com/zahlmann/jarvis-phi/internal/store"
	"github.com/zahlmann/jarvis-phi/internal/telegram"
	"github.com/zahlmann/phi/ai/model"
)

type app struct {
	cfg      config.Config
	logger   *logstore.Store
	tg       *telegram.Client
	runtime  *runtime.Service
	dedup    *store.DedupStore
	msgIndex *store.MessageIndex
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	ensureBinPath()
	ensureJarvisctlAvailable()

	logger, err := logstore.New(filepath.Join(cfg.DataDir, "logs"))
	if err != nil {
		log.Fatalf("log store error: %v", err)
	}

	dedup, err := store.NewDedupStore(filepath.Join(cfg.DataDir, "messages", "dedup.json"))
	if err != nil {
		log.Fatalf("dedup store error: %v", err)
	}

	msgIndex, err := store.NewMessageIndex(filepath.Join(cfg.DataDir, "messages", "index.json"))
	if err != nil {
		log.Fatalf("message index error: %v", err)
	}
	memStore, err := memory.NewStore(filepath.Join(cfg.DataDir, "memory", "memories.parquet"))
	if err != nil {
		log.Fatalf("memory store error: %v", err)
	}
	memEmbedder, err := memory.NewOpenAIEmbedder(cfg.OpenAIAPIKey, cfg.MemoryEmbeddingModel)
	if err != nil {
		log.Fatalf("memory embedder error: %v", err)
	}

	tgClient := telegram.NewClient(cfg.TelegramBotToken, cfg.TelegramAPIBase)
	rt := runtime.New(cfg, logger)

	application := &app{
		cfg:      cfg,
		logger:   logger,
		tg:       tgClient,
		runtime:  rt,
		dedup:    dedup,
		msgIndex: msgIndex,
	}

	schedStore, err := scheduler.NewStore(filepath.Join(cfg.DataDir, "scheduler", "jobs.json"))
	if err != nil {
		log.Fatalf("scheduler store error: %v", err)
	}
	heartbeat, err := scheduler.NewHeartbeat(
		filepath.Join(cfg.DataDir, "heartbeat", "state.json"),
		cfg.HeartbeatEnabled,
		cfg.DefaultChatID,
		cfg.HeartbeatPrompt,
	)
	if err != nil {
		log.Fatalf("heartbeat init error: %v", err)
	}

	engine := scheduler.NewEngine(
		schedStore,
		heartbeat,
		func(ctx context.Context, trigger scheduler.Trigger) error {
			application.runtime.Enqueue(runtime.PromptInput{
				ChatID:   trigger.ChatID,
				UserName: "scheduler",
				Message:  trigger.Prompt,
				Source:   trigger.Source,
			})
			return nil
		},
		application.runtime.IsBusy,
		logger,
	)
	if err := engine.Require(); err != nil {
		log.Fatalf("scheduler config error: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	engine.Start(ctx)
	go runMemoryEmbeddingLoop(ctx, memStore, memEmbedder, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", application.healthz)
	mux.HandleFunc("/telegram/webhook", application.webhook)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	_ = logger.Write("server", "startup", map[string]any{"listen": cfg.ListenAddr})
	log.Printf("jarvis-phi server listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	_ = logger.Write("server", "shutdown", map[string]any{})
}

func (a *app) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "jarvis-phi"})
}

func (a *app) webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if a.cfg.TelegramWebhookToken != "" {
		h := strings.TrimSpace(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"))
		if h == "" || h != a.cfg.TelegramWebhookToken {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "invalid webhook secret"})
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return
	}

	update, err := telegram.ParseUpdate(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid telegram payload"})
		return
	}

	normalized, err := telegram.NormalizeUpdate(update)
	if err != nil {
		_ = a.logger.Write("telegram", "normalize_error", map[string]any{"error": err.Error()})
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid message"})
		return
	}
	if normalized == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": true})
		return
	}

	dedupID := fmt.Sprintf("update:%d", normalized.UpdateID)
	if a.dedup.Seen(dedupID) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true})
		return
	}
	if err := a.dedup.Mark(dedupID); err != nil {
		_ = a.logger.Write("telegram", "dedup_mark_error", map[string]any{"error": err.Error(), "update_id": normalized.UpdateID})
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	go a.processNormalized(*normalized)
}

func (a *app) processNormalized(n telegram.NormalizedUpdate) {
	replyTo := ""
	if n.ReplyToMessageID != 0 {
		if rec, ok := a.msgIndex.Get(n.ChatID, n.ReplyToMessageID); ok {
			replyTo = rec.Text
		}
	}

	input := runtime.PromptInput{
		ChatID:   n.ChatID,
		UserName: n.UserName,
		Source:   "telegram",
		ReplyTo:  replyTo,
		IsVoice:  n.Type == "voice",
	}

	switch n.Type {
	case "text":
		input.Message = n.Text
	case "voice":
		if !a.cfg.TranscriptionEnabled {
			_ = a.logger.Write("telegram", "voice_transcription_disabled", map[string]any{
				"chat_id": n.ChatID,
			})
			if _, err := a.tg.SendText(n.ChatID, "Voice transcription is disabled in this Jarvis setup."); err != nil {
				_ = a.logger.Write("telegram", "voice_transcription_disabled_send_error", map[string]any{
					"chat_id": n.ChatID,
					"error":   err.Error(),
				})
			}
			return
		}
		data, contentType, err := a.tg.DownloadFile(n.VoiceFileID)
		if err != nil {
			_ = a.logger.Write("telegram", "voice_download_error", map[string]any{"chat_id": n.ChatID, "error": err.Error()})
			return
		}
		text, err := media.TranscribeVoice(context.Background(), a.cfg.OpenAIAPIKey, data, contentType)
		if err != nil {
			_ = a.logger.Write("telegram", "transcription_error", map[string]any{"chat_id": n.ChatID, "error": err.Error()})
			return
		}
		input.Message = text
	case "photo":
		data, contentType, err := a.tg.DownloadFile(n.PhotoFileID)
		if err != nil {
			_ = a.logger.Write("telegram", "photo_download_error", map[string]any{"chat_id": n.ChatID, "error": err.Error()})
			return
		}
		input.Message = n.Caption
		input.Images = []model.ImageContent{{
			Type:     model.ContentImage,
			MIMEType: contentType,
			Data:     base64.StdEncoding.EncodeToString(data),
		}}
	default:
		return
	}

	if strings.TrimSpace(input.Message) == "" {
		input.Message = "(empty message)"
	}

	_ = a.msgIndex.Put(store.MessageRecord{
		ChatID:    n.ChatID,
		MessageID: n.MessageID,
		Direction: "inbound",
		Sender:    n.UserName,
		Text:      input.Message,
	})

	_ = a.logger.Write("telegram", "inbound_message", map[string]any{
		"chat_id":    n.ChatID,
		"message_id": n.MessageID,
		"type":       n.Type,
		"user":       n.UserName,
	})

	a.runtime.Enqueue(input)
}

func ensureJarvisctlAvailable() {
	if _, err := exec.LookPath("jarvisctl"); err == nil {
		return
	}
	log.Fatalf("jarvisctl is required but was not found in PATH; build it with `go build -o ./bin/jarvisctl ./cmd/jarvisctl` or run `./wake-jarvis.sh`")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func ensureBinPath() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	binDir := filepath.Join(cwd, "bin")
	info, err := os.Stat(binDir)
	if err != nil || !info.IsDir() {
		return
	}

	current := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(current) {
		if filepath.Clean(entry) == binDir {
			return
		}
	}

	if current == "" {
		_ = os.Setenv("PATH", binDir)
		return
	}
	_ = os.Setenv("PATH", binDir+string(os.PathListSeparator)+current)
}

func runMemoryEmbeddingLoop(ctx context.Context, st *memory.Store, embedder memory.Embedder, logger *logstore.Store) {
	if st == nil || embedder == nil {
		return
	}

	run := func() {
		runCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
		defer cancel()

		updated, err := st.BackfillEmbeddings(runCtx, embedder, 20)
		if err != nil {
			if runCtx.Err() != nil || ctx.Err() != nil {
				return
			}
			_ = logger.Write("memory", "embed_backfill_error", map[string]any{"error": err.Error()})
			return
		}
		if updated > 0 {
			_ = logger.Write("memory", "embed_backfill_ok", map[string]any{"updated": updated})
		}
	}

	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
