package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxTelegramTextLength = 4096

type Client struct {
	botToken string
	baseURL  string
	http     *http.Client
}

func NewClient(botToken, apiBase string) *Client {
	apiBase = strings.TrimRight(apiBase, "/")
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	return &Client{
		botToken: botToken,
		baseURL:  apiBase,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type sendResponse struct {
	OK          bool      `json:"ok"`
	Description string    `json:"description,omitempty"`
	Result      tgMessage `json:"result"`
}

type tgMessage struct {
	MessageID int64 `json:"message_id"`
}

type SendResult struct {
	MessageID int64
}

func (c *Client) SendText(chatID int64, text string) (SendResult, error) {
	chunks := splitText(text, maxTelegramTextLength)
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	var out SendResult
	for _, chunk := range chunks {
		res, err := c.sendJSON("sendMessage", map[string]any{
			"chat_id": chatID,
			"text":    chunk,
		})
		if err != nil {
			return SendResult{}, err
		}
		out = SendResult{MessageID: res.Result.MessageID}
	}
	return out, nil
}

func (c *Client) SendAudioFile(chatID int64, path string, caption string) (SendResult, error) {
	fields := map[string]string{
		"chat_id": fmt.Sprintf("%d", chatID),
	}
	if strings.TrimSpace(caption) != "" {
		fields["caption"] = caption
	}
	return c.sendMultipartFile("sendAudio", "audio", path, fields)
}

func (c *Client) SendPhotoFile(chatID int64, path string, caption string) (SendResult, error) {
	fields := map[string]string{
		"chat_id": fmt.Sprintf("%d", chatID),
	}
	if strings.TrimSpace(caption) != "" {
		fields["caption"] = caption
	}
	return c.sendMultipartFile("sendPhoto", "photo", path, fields)
}

func (c *Client) sendJSON(method string, payload map[string]any) (sendResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return sendResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint(method), bytes.NewReader(body))
	if err != nil {
		return sendResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return sendResponse{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return sendResponse{}, err
	}

	var out sendResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return sendResponse{}, fmt.Errorf("decode telegram response: %w", err)
	}
	if !out.OK {
		return sendResponse{}, fmt.Errorf("telegram %s failed: %s", method, out.Description)
	}
	return out, nil
}

func (c *Client) sendMultipartFile(method, fieldName, path string, fields map[string]string) (SendResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return SendResult{}, err
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return SendResult{}, err
		}
	}

	part, err := writer.CreateFormFile(fieldName, filepath.Base(path))
	if err != nil {
		return SendResult{}, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return SendResult{}, err
	}
	if err := writer.Close(); err != nil {
		return SendResult{}, err
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint(method), &body)
	if err != nil {
		return SendResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return SendResult{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return SendResult{}, err
	}

	var out sendResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return SendResult{}, fmt.Errorf("decode telegram response: %w", err)
	}
	if !out.OK {
		return SendResult{}, fmt.Errorf("telegram %s failed: %s", method, out.Description)
	}
	return SendResult{MessageID: out.Result.MessageID}, nil
}

type getFileResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
	Result      struct {
		FilePath string `json:"file_path"`
	} `json:"result"`
}

func (c *Client) DownloadFile(fileID string) ([]byte, string, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, "", fmt.Errorf("file id is required")
	}
	body, err := json.Marshal(map[string]any{"file_id": fileID})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.endpoint("getFile"), bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, "", err
	}

	var meta getFileResponse
	if err := json.Unmarshal(respBody, &meta); err != nil {
		return nil, "", fmt.Errorf("decode getFile response: %w", err)
	}
	if !meta.OK {
		return nil, "", fmt.Errorf("telegram getFile failed: %s", meta.Description)
	}
	if strings.TrimSpace(meta.Result.FilePath) == "" {
		return nil, "", fmt.Errorf("telegram getFile returned empty file_path")
	}

	url := fmt.Sprintf("%s/file/bot%s/%s", strings.TrimRight(c.baseURL, "/"), c.botToken, meta.Result.FilePath)
	httpResp, err = c.http.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer httpResp.Body.Close()

	contentType := httpResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func (c *Client) endpoint(method string) string {
	return fmt.Sprintf("%s/bot%s/%s", strings.TrimRight(c.baseURL, "/"), c.botToken, method)
}

func splitText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	chunks := []string{}
	remaining := text
	for len(remaining) > maxLen {
		splitAt := strings.LastIndex(remaining[:maxLen], "\n\n")
		if splitAt < maxLen/2 {
			splitAt = strings.LastIndex(remaining[:maxLen], "\n")
		}
		if splitAt < maxLen/2 {
			splitAt = strings.LastIndex(remaining[:maxLen], " ")
		}
		if splitAt <= 0 {
			splitAt = maxLen
		}
		chunks = append(chunks, strings.TrimSpace(remaining[:splitAt]))
		remaining = strings.TrimSpace(remaining[splitAt:])
	}
	if remaining != "" {
		chunks = append(chunks, remaining)
	}
	return chunks
}
