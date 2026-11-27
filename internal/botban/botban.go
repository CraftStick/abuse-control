package botban

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func New(url, token string) *Client {
	return &Client{URL: url, Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) Ban(ctx context.Context, telegramID int64, reason string) (string, error) {
	body, err := json.Marshal(map[string]any{"telegram_id": telegramID, "reason": reason})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("botban: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("botban: http %d", resp.StatusCode)
	}
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Result == "" {
		return "", fmt.Errorf("botban: bad response")
	}
	return out.Result, nil
}
