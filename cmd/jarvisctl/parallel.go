package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zahlmann/jarvis-phi/internal/cli"
	"github.com/zahlmann/jarvis-phi/internal/config"
	parallelapi "github.com/zahlmann/jarvis-phi/internal/parallel"
)

func handleParallel(args []string) {
	if len(args) < 1 {
		cli.Exitf("parallel subcommand required")
	}

	cfg, err := config.LoadWithOptions(config.LoadOptions{
		RequireTelegramToken:  false,
		RequirePhiCredentials: false,
	})
	if err != nil {
		cli.Exitf("config error: %v", err)
	}

	client, err := parallelapi.NewClient(cfg.ParallelAPIKey)
	if err != nil {
		cli.Exitf("parallel config error: %v", err)
	}

	switch args[0] {
	case "search":
		handleParallelSearch(args[1:], client)
	case "extract":
		handleParallelExtract(args[1:], client)
	default:
		cli.Exitf("unknown parallel command: %s", args[0])
	}
}

func handleParallelSearch(args []string, client *parallelapi.Client) {
	fs := flag.NewFlagSet("parallel search", flag.ExitOnError)
	objective := fs.String("objective", "", "search objective")
	payloadRaw := fs.String("payload", "", "raw JSON object payload")
	payloadFile := fs.String("payload-file", "", "path to JSON payload file or - for stdin")
	_ = fs.Parse(args)

	payload, err := loadJSONObject(*payloadRaw, *payloadFile)
	if err != nil {
		cli.Exitf("parallel search payload error: %v", err)
	}

	text := strings.TrimSpace(*objective)
	if text != "" {
		payload["objective"] = text
	}

	if !hasNonEmptyString(payload, "objective") {
		cli.Exitf("--objective is required unless provided in --payload or --payload-file")
	}

	resp, err := client.Search(context.Background(), payload)
	if err != nil {
		cli.Exitf("parallel search failed: %v", err)
	}
	cli.PrintJSON(resp)
}

func handleParallelExtract(args []string, client *parallelapi.Client) {
	fs := flag.NewFlagSet("parallel extract", flag.ExitOnError)
	var urls stringListFlag
	fs.Var(&urls, "url", "url to extract; repeat the flag for multiple urls")
	objective := fs.String("objective", "", "optional extraction objective")
	excerpts := fs.Bool("excerpts", true, "include excerpt snippets")
	fullContent := fs.Bool("full-content", false, "include full markdown content")
	payloadRaw := fs.String("payload", "", "raw JSON object payload")
	payloadFile := fs.String("payload-file", "", "path to JSON payload file or - for stdin")
	_ = fs.Parse(args)

	payload, err := loadJSONObject(*payloadRaw, *payloadFile)
	if err != nil {
		cli.Exitf("parallel extract payload error: %v", err)
	}

	if len(urls) > 0 {
		payload["urls"] = []string(urls)
	}

	text := strings.TrimSpace(*objective)
	if text != "" {
		payload["objective"] = text
	}

	if _, ok := payload["excerpts"]; !ok {
		payload["excerpts"] = *excerpts
	}
	if _, ok := payload["full_content"]; !ok {
		payload["full_content"] = *fullContent
	}

	if !hasNonEmptyList(payload, "urls") {
		cli.Exitf("at least one --url is required unless provided in --payload or --payload-file")
	}

	resp, err := client.Extract(context.Background(), payload)
	if err != nil {
		cli.Exitf("parallel extract failed: %v", err)
	}
	cli.PrintJSON(resp)
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func loadJSONObject(raw, path string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	path = strings.TrimSpace(path)

	if raw != "" && path != "" {
		return nil, fmt.Errorf("use only one of --payload or --payload-file")
	}

	if raw == "" && path == "" {
		return map[string]any{}, nil
	}

	if raw != "" {
		return decodeJSONObject([]byte(raw))
	}

	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	return decodeJSONObject(data)
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("payload must be a JSON object")
	}
	return payload, nil
}

func hasNonEmptyString(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}

	text, ok := value.(string)
	if !ok {
		return false
	}

	return strings.TrimSpace(text) != ""
}

func hasNonEmptyList(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}

	switch items := value.(type) {
	case []string:
		return len(items) > 0
	case []any:
		return len(items) > 0
	default:
		return false
	}
}
