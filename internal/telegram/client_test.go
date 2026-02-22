package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSetWebhook(t *testing.T) {
	t.Parallel()

	var gotURL string
	var gotSecret string
	var gotPath string

	client := NewClient("test-token", "https://api.telegram.org")
	client.http = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			gotPath = r.URL.Path

			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			gotURL = payload["url"]
			gotSecret = payload["secret_token"]

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	if err := client.SetWebhook("https://example.com/telegram/webhook", "my-secret"); err != nil {
		t.Fatalf("SetWebhook returned error: %v", err)
	}
	if gotPath != "/bottest-token/setWebhook" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotURL != "https://example.com/telegram/webhook" {
		t.Fatalf("unexpected url payload: %q", gotURL)
	}
	if gotSecret != "my-secret" {
		t.Fatalf("unexpected secret payload: %q", gotSecret)
	}
}

func TestSetWebhookRequiresURL(t *testing.T) {
	t.Parallel()

	client := NewClient("test-token", "https://api.telegram.org")
	if err := client.SetWebhook("", "my-secret"); err == nil {
		t.Fatalf("expected error for empty url")
	}
}

func TestSendTyping(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotChatID float64
	var gotAction string

	client := NewClient("test-token", "https://api.telegram.org")
	client.http = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			gotPath = r.URL.Path

			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			gotChatID, _ = payload["chat_id"].(float64)
			gotAction, _ = payload["action"].(string)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	if err := client.SendTyping(12345); err != nil {
		t.Fatalf("SendTyping returned error: %v", err)
	}
	if gotPath != "/bottest-token/sendChatAction" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotChatID != 12345 {
		t.Fatalf("unexpected chat_id payload: %v", gotChatID)
	}
	if gotAction != "typing" {
		t.Fatalf("unexpected action payload: %q", gotAction)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
