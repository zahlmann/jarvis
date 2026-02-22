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

func TestBatchUpdateV2Payload(t *testing.T) {
	var seen map[string]any
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.String() == "https://bring.test/rest/v2/bringauth":
			return jsonResp(http.StatusOK, map[string]any{
				"uuid":         "u1",
				"publicUuid":   "p1",
				"access_token": "tok",
				"token_type":   "Bearer",
			})
		case r.Method == http.MethodPut && r.URL.String() == "https://bring.test/rest/v2/bringlists/list-1/items":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &seen)
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
	changes, ok := seen["changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("unexpected changes payload: %#v", seen["changes"])
	}
	change, ok := changes[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected change entry: %#v", changes[0])
	}
	if got, _ := change["operation"].(string); got != opAdd {
		t.Fatalf("unexpected operation: %q", got)
	}
	if got, _ := change["itemId"].(string); got != "milk" {
		t.Fatalf("unexpected itemId: %q", got)
	}
	if got, _ := change["spec"].(string); got != "whole" {
		t.Fatalf("unexpected spec: %q", got)
	}
}

func TestRunListUsesAuthListUUIDWhenListLookupUnavailable(t *testing.T) {
	t.Setenv("BRING_EMAIL", "a@example.com")
	t.Setenv("BRING_PASSWORD", "pw")
	t.Setenv("BRING_ITEM_CONTEXT", "en-US")
	t.Setenv("BRING_API_BASE_URL", "https://bring.test/rest")
	t.Setenv("BRING_LIST_UUID", "")
	t.Setenv("BRING_LIST_NAME", "")

	origDefault := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.String() == "https://bring.test/rest/v2/bringauth":
			return jsonResp(http.StatusOK, map[string]any{
				"uuid":          "u1",
				"publicUuid":    "p1",
				"bringListUUID": "list-auth",
				"access_token":  "tok",
				"token_type":    "Bearer",
			})
		case r.Method == http.MethodGet && r.URL.String() == "https://bring.test/rest/v2/bringlists/list-auth":
			return jsonResp(http.StatusOK, map[string]any{
				"purchase": []map[string]any{{"name": "milk", "specification": "2%"}},
				"recently": []map[string]any{},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/bringusers/"):
			t.Fatalf("unexpected list lookup endpoint call: %s", r.URL.String())
			return nil, nil
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
	if !strings.Contains(out, "\"list_name\": \"Bring list\"") {
		t.Fatalf("unexpected list name output: %s", out)
	}
	if !strings.Contains(out, "\"name\": \"milk\"") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestBatchUpdateFallbackToLegacyEndpoint(t *testing.T) {
	calls := []string{}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.String() == "https://bring.test/rest/v2/bringauth":
			return jsonResp(http.StatusOK, map[string]any{
				"uuid":         "u1",
				"publicUuid":   "p1",
				"access_token": "tok",
				"token_type":   "Bearer",
			})
		case r.Method == http.MethodPut && r.URL.String() == "https://bring.test/rest/v2/bringlists/list-1/items":
			calls = append(calls, "v2")
			return jsonResp(http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.String(), "https://bring.test/rest/bringlists/list-1?"):
			calls = append(calls, "legacy")
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
	if len(calls) != 2 || calls[0] != "v2" || calls[1] != "legacy" {
		t.Fatalf("unexpected endpoint call order: %v", calls)
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
