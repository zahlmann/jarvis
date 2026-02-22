package bring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL = "https://api.getbring.com/rest"
	authPath          = "/v2/bringauth"
	defaultLocale     = "en-US"

	opAdd      = "TO_PURCHASE"
	opComplete = "TO_RECENTLY"
	opRemove   = "REMOVE"
)

var defaultHeaders = map[string]string{
	"X-BRING-API-KEY":       "9f8e7d4c-5554-3f9a-86fd-33225d4f35ad",
	"X-BRING-CLIENT":        "webApp",
	"X-BRING-COUNTRY":       "US",
	"X-BRING-RESPONSE-TYPE": "JSON",
	"Host":                  "api.getbring.com",
}

type Client struct {
	baseURL     string
	httpClient  *http.Client
	email       string
	password    string
	listUUID    string
	listName    string
	itemContext string

	uuid       string
	publicUUID string
	headers    map[string]string
}

type authResponse struct {
	UUID        string `json:"uuid"`
	PublicUUID  string `json:"publicUuid"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type listDescriptor struct {
	ListUUID string `json:"listUuid"`
	Name     string `json:"name"`
}

type listResponse struct {
	Lists []listDescriptor `json:"lists"`
}

type normalizedItem struct {
	Name string `json:"name"`
	Spec string `json:"spec"`
	UUID string `json:"uuid,omitempty"`
}

type normalizedList struct {
	ListName          string           `json:"list_name"`
	Items             []normalizedItem `json:"items"`
	RecentlyCompleted []normalizedItem `json:"recently_completed"`
}

func Run(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("bring subcommand required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := newClientFromEnv()
	if err != nil {
		return "", err
	}
	if err := client.login(ctx); err != nil {
		return "", err
	}

	listID, listName, err := client.resolveList(ctx)
	if err != nil {
		return "", err
	}

	sub := args[0]
	switch sub {
	case "list":
		asJSON := len(args) > 1 && strings.TrimSpace(args[1]) == "--json"
		norm, err := client.list(ctx, listID, listName)
		if err != nil {
			return "", err
		}
		if asJSON {
			raw, err := json.MarshalIndent(norm, "", "  ")
			if err != nil {
				return "", err
			}
			return string(raw), nil
		}
		return formatList(norm), nil

	case "add":
		if len(args) < 2 {
			return "", fmt.Errorf("bring add requires at least one item")
		}
		added := []string{}
		for _, raw := range args[1:] {
			name, spec := parseItemWithSpec(raw)
			if strings.TrimSpace(name) == "" {
				continue
			}
			if err := client.batchUpdate(ctx, listID, name, spec, "", opAdd); err != nil {
				return "", err
			}
			added = append(added, name)
		}
		return jsonObject(map[string]any{"added": added, "list": listName})

	case "remove":
		if len(args) < 2 {
			return "", fmt.Errorf("bring remove requires at least one item")
		}
		removed := []string{}
		for _, raw := range args[1:] {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if err := client.batchUpdate(ctx, listID, name, "", "", opRemove); err != nil {
				return "", err
			}
			removed = append(removed, name)
		}
		return jsonObject(map[string]any{"removed": removed, "list": listName})

	case "complete":
		if len(args) < 2 {
			return "", fmt.Errorf("bring complete requires at least one item")
		}
		completed := []string{}
		for _, raw := range args[1:] {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if err := client.batchUpdate(ctx, listID, name, "", "", opComplete); err != nil {
				return "", err
			}
			completed = append(completed, name)
		}
		return jsonObject(map[string]any{"completed": completed, "list": listName})

	default:
		return "", fmt.Errorf("unknown bring command: %s", sub)
	}
}

func newClientFromEnv() (*Client, error) {
	email := strings.TrimSpace(os.Getenv("BRING_EMAIL"))
	password := strings.TrimSpace(os.Getenv("BRING_PASSWORD"))
	if email == "" || password == "" {
		return nil, fmt.Errorf("BRING_EMAIL and BRING_PASSWORD must be set")
	}

	baseURL := strings.TrimSpace(os.Getenv("BRING_API_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	itemContext := strings.TrimSpace(os.Getenv("BRING_ITEM_CONTEXT"))
	if itemContext == "" {
		itemContext = defaultLocale
	}

	headers := map[string]string{}
	for k, v := range defaultHeaders {
		headers[k] = v
	}
	if country := strings.TrimSpace(os.Getenv("BRING_COUNTRY")); country != "" {
		headers["X-BRING-COUNTRY"] = country
	}

	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  &http.Client{Timeout: 20 * time.Second},
		email:       email,
		password:    password,
		listUUID:    strings.TrimSpace(os.Getenv("BRING_LIST_UUID")),
		listName:    strings.TrimSpace(os.Getenv("BRING_LIST_NAME")),
		itemContext: itemContext,
		headers:     headers,
	}, nil
}

func (c *Client) login(ctx context.Context) error {
	values := url.Values{}
	values.Set("email", c.email)
	values.Set("password", c.password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+authPath, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bring login failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bring login failed: status=%d", resp.StatusCode)
	}

	var auth authResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		return fmt.Errorf("bring login decode failed: %w", err)
	}
	if strings.TrimSpace(auth.UUID) == "" || strings.TrimSpace(auth.AccessToken) == "" {
		return errors.New("bring login response missing required fields")
	}

	c.uuid = auth.UUID
	c.publicUUID = auth.PublicUUID
	c.headers["X-BRING-USER-UUID"] = auth.UUID
	if strings.TrimSpace(auth.PublicUUID) != "" {
		c.headers["X-BRING-PUBLIC-USER-UUID"] = auth.PublicUUID
	}
	tokenType := strings.TrimSpace(auth.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	c.headers["Authorization"] = tokenType + " " + auth.AccessToken
	return nil
}

func (c *Client) resolveList(ctx context.Context) (string, string, error) {
	if strings.TrimSpace(c.uuid) == "" {
		return "", "", errors.New("not authenticated")
	}

	endpoint := c.baseURL + "/bringusers/" + url.PathEscape(c.uuid) + "/lists"
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("bring list lookup failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("bring list lookup failed: status=%d", resp.StatusCode)
	}

	var lists listResponse
	if err := json.NewDecoder(resp.Body).Decode(&lists); err != nil {
		return "", "", fmt.Errorf("bring list lookup decode failed: %w", err)
	}
	if len(lists.Lists) == 0 {
		return "", "", errors.New("no Bring shopping lists found")
	}

	if c.listUUID != "" {
		for _, l := range lists.Lists {
			if l.ListUUID == c.listUUID {
				return l.ListUUID, l.Name, nil
			}
		}
		return "", "", fmt.Errorf("BRING_LIST_UUID=%s not found", c.listUUID)
	}
	if c.listName != "" {
		for _, l := range lists.Lists {
			if strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(c.listName)) {
				return l.ListUUID, l.Name, nil
			}
		}
		return "", "", fmt.Errorf("BRING_LIST_NAME=%q not found", c.listName)
	}

	return lists.Lists[0].ListUUID, lists.Lists[0].Name, nil
}

func (c *Client) list(ctx context.Context, listUUID, listName string) (normalizedList, error) {
	endpoint := c.baseURL + "/v2/bringlists/" + url.PathEscape(listUUID)
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return normalizedList{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return normalizedList{}, fmt.Errorf("bring list fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return normalizedList{}, fmt.Errorf("bring list fetch failed: status=%d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return normalizedList{}, fmt.Errorf("bring list decode failed: %w", err)
	}

	purchase, recently := normalizeItems(payload)
	return normalizedList{
		ListName:          listName,
		Items:             purchase,
		RecentlyCompleted: recently,
	}, nil
}

func (c *Client) batchUpdate(ctx context.Context, listUUID, itemID, spec, itemUUID, operation string) error {
	endpoint := c.baseURL + "/bringlists/" + url.PathEscape(listUUID)
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("listUuid", listUUID)
	q.Set("itemId", itemID)
	q.Set("specification", spec)
	q.Set("changes", "0")
	q.Set("operationType", operation)
	q.Set("itemContext", c.itemContext)
	if strings.TrimSpace(itemUUID) != "" {
		q.Set("uuid", itemUUID)
	}
	u.RawQuery = q.Encode()

	req, err := c.newRequest(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bring %s failed: %w", strings.ToLower(operation), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bring %s failed: status=%d", strings.ToLower(operation), resp.StatusCode)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func normalizeItems(payload map[string]any) ([]normalizedItem, []normalizedItem) {
	base := payload
	if nested, ok := payload["items"].(map[string]any); ok {
		base = nested
	}
	return normalizeItemSlice(base["purchase"]), normalizeItemSlice(base["recently"])
}

func normalizeItemSlice(raw any) []normalizedItem {
	arr, ok := raw.([]any)
	if !ok {
		return []normalizedItem{}
	}
	out := make([]normalizedItem, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["itemId"].(string)
		spec, _ := m["specification"].(string)
		uuid, _ := m["uuid"].(string)
		out = append(out, normalizedItem{Name: name, Spec: spec, UUID: uuid})
	}
	return out
}

func parseItemWithSpec(raw string) (string, string) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func jsonObject(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatList(list normalizedList) string {
	if len(list.Items) == 0 {
		return "List is empty"
	}
	b := strings.Builder{}
	b.WriteString(list.ListName)
	b.WriteString(":\n")
	for _, item := range list.Items {
		b.WriteString("- ")
		b.WriteString(item.Name)
		if strings.TrimSpace(item.Spec) != "" {
			b.WriteString(" (")
			b.WriteString(item.Spec)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
