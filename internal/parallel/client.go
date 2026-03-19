package parallel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.parallel.ai"

type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func NewClient(apiKey string) (*Client, error) {
	return newClient(apiKey, defaultBaseURL, &http.Client{Timeout: 60 * time.Second})
}

func newClient(apiKey, baseURL string, httpClient *http.Client) (*Client, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("PARALLEL_API_KEY is required")
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    httpClient,
	}, nil
}

func (c *Client) Search(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.postJSON(ctx, "/v1beta/search", payload)
}

func (c *Client) Extract(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.postJSON(ctx, "/v1beta/extract", payload)
}

func (c *Client) postJSON(ctx context.Context, path string, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		payload = map[string]any{}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("parallel request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if len(respBody) == 0 {
		return map[string]any{}, nil
	}

	var out map[string]any
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}
