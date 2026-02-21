package bring

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestRunListWithTransportMock(t *testing.T) {
	t.Setenv("BRING_EMAIL", "a@example.com")
	t.Setenv("BRING_PASSWORD", "pw")
	t.Setenv("BRING_LIST_UUID", "list-1")
	t.Setenv("BRING_ITEM_CONTEXT", "en-US")
	t.Setenv("BRING_API_BASE_URL", "https://bring.test/rest")

	origDefault := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.String() == "https://bring.test/rest/v2/bringauth":
			body, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(body))
			if values.Get("email") != "a@example.com" || values.Get("password") != "pw" {
				t.Fatalf("unexpected login form: %v", values)
			}
			return jsonResp(http.StatusOK, map[string]any{
				"uuid":         "u1",
				"publicUuid":   "p1",
				"access_token": "tok",
				"token_type":   "Bearer",
			})
		case r.Method == http.MethodGet && r.URL.String() == "https://bring.test/rest/bringusers/u1/lists":
			return jsonResp(http.StatusOK, map[string]any{
				"lists": []map[string]any{{"listUuid": "list-1", "name": "Main"}},
			})
		case r.Method == http.MethodGet && r.URL.String() == "https://bring.test/rest/v2/bringlists/list-1":
			return jsonResp(http.StatusOK, map[string]any{
				"purchase": []map[string]any{{"itemId": "milk", "specification": "2%"}},
				"recently": []map[string]any{{"itemId": "eggs", "specification": ""}},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = origDefault }()

	out, err := Run([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(out, "milk") || !strings.Contains(out, "eggs") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestBatchUpdateQuery(t *testing.T) {
	var seen url.Values
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.String() == "https://bring.test/rest/v2/bringauth":
			return jsonResp(http.StatusOK, map[string]any{
				"uuid":         "u1",
				"publicUuid":   "p1",
				"access_token": "tok",
				"token_type":   "Bearer",
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.String(), "https://bring.test/rest/bringlists/list-1?"):
			seen = r.URL.Query()
			return jsonResp(http.StatusOK, map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})

	c := &Client{
		baseURL:     "https://bring.test/rest",
		httpClient:  &http.Client{Transport: transport},
		email:       "a@example.com",
		password:    "pw",
		itemContext: "en-US",
		headers:     map[string]string{},
	}
	if err := c.login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if err := c.batchUpdate(context.Background(), "list-1", "milk", "whole", "", opAdd); err != nil {
		t.Fatalf("batchUpdate failed: %v", err)
	}
	if got := seen.Get("operationType"); got != opAdd {
		t.Fatalf("unexpected operationType: %q", got)
	}
	if got := seen.Get("itemContext"); got != "en-US" {
		t.Fatalf("unexpected itemContext: %q", got)
	}
	if got := seen.Get("itemId"); got != "milk" {
		t.Fatalf("unexpected itemId: %q", got)
	}
}

func jsonResp(status int, payload any) (*http.Response, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(b)),
	}, nil
}
