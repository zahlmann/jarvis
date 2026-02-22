package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const DefaultEmbeddingModel = "text-embedding-3-small"

type Embedder interface {
	Embed(ctx context.Context, input string) ([]float32, error)
}

type OpenAIEmbedder struct {
	apiKey string
	model  string
	client *http.Client
}

func NewOpenAIEmbedder(apiKey, model string) (*OpenAIEmbedder, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultEmbeddingModel
	}
	return &OpenAIEmbedder{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 45 * time.Second},
	}, nil
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, input string) ([]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("embedder is nil")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("embedding input is required")
	}

	reqBody, err := json.Marshal(map[string]any{
		"model": e.model,
		"input": input,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 || len(payload.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding response did not contain vectors")
	}

	return NormalizeEmbedding(payload.Data[0].Embedding)
}

func NormalizeEmbedding(raw []float32) ([]float32, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("embedding vector is empty")
	}
	normSq := 0.0
	for _, v := range raw {
		fv := float64(v)
		normSq += fv * fv
	}
	if normSq == 0 {
		return nil, fmt.Errorf("embedding vector has zero norm")
	}

	norm := math.Sqrt(normSq)
	out := make([]float32, len(raw))
	for i := range raw {
		out[i] = float32(float64(raw[i]) / norm)
	}
	return out, nil
}

func DotProduct(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for i := range a {
		sum += float64(a[i] * b[i])
	}
	return sum
}
