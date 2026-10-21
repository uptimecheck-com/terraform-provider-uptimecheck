// Package client talks to the Uptimecheck REST API (/api/v1) with an API key.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Endpoint  string
	APIKey    string
	UserAgent string
	HTTP      *http.Client
}

func New(endpoint, apiKey, userAgent string) *Client {
	return &Client{Endpoint: strings.TrimRight(endpoint, "/"), APIKey: apiKey, UserAgent: userAgent, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Error is an API error answer: {"error": {"code", "message", "fields"}}.
type Error struct {
	Status  int
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("%s (HTTP %d, %s)", e.Message, e.Status, e.Code)
	for field, problem := range e.Fields {
		msg += fmt.Sprintf("\n  %s: %s", field, problem)
	}
	return msg
}

// NotFound reports whether err is a 404 from the API (the object was deleted outside Terraform).
func NotFound(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Status == http.StatusNotFound
}

// Do sends a request and decodes {"data": …} into out (when out is not nil). Rate-limited
// requests (429) are retried after Retry-After, up to three times.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
		res.Body.Close()
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			wait, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if wait <= 0 || wait > 60 {
				wait = 5
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		if res.StatusCode >= 400 {
			var wrapped struct {
				Error Error `json:"error"`
			}
			if json.Unmarshal(data, &wrapped) != nil || wrapped.Error.Message == "" {
				wrapped.Error = Error{Code: "http_error", Message: strings.TrimSpace(string(data))}
			}
			wrapped.Error.Status = res.StatusCode
			return &wrapped.Error
		}
		if out == nil {
			return nil
		}
		var wrapped struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return fmt.Errorf("unexpected answer from the API: %w", err)
		}
		return json.Unmarshal(wrapped.Data, out)
	}
}

// Monitor as the API returns it (the fields the provider uses).
type Monitor struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	Target          *string         `json:"target"`
	Port            *int64          `json:"port"`
	Config          json.RawMessage `json:"config"`
	IntervalSeconds int64           `json:"intervalSeconds"`
	Regions         []string        `json:"regions"`
	ProjectID       *string         `json:"projectId"`
	ChannelIDs      []string        `json:"channelIds"`
	Status          string          `json:"status"`
	HeartbeatURL    *string         `json:"heartbeatUrl"`
}

// MonitorInput is what POST accepts and PATCH merges. Regions and ChannelIDs are left out when
// nil (the API rejects null): POST then uses [] and the workspace's default alert channels, PATCH
// keeps the current ones. A pointer to an empty list sends [].
type MonitorInput struct {
	Type            string          `json:"type,omitempty"`
	Name            string          `json:"name"`
	Target          *string         `json:"target,omitempty"`
	Port            *int64          `json:"port,omitempty"`
	IntervalSeconds *int64          `json:"intervalSeconds,omitempty"`
	Regions         *[]string       `json:"regions,omitempty"`
	ProjectID       *string         `json:"projectId"`
	ChannelIDs      *[]string       `json:"channelIds,omitempty"`
	Config          json.RawMessage `json:"config,omitempty"`
}

type AlertChannel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	IsDefault bool   `json:"isDefault"`
	Enabled   bool   `json:"enabled"`
}

type Project struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type StatusPage struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Published bool    `json:"published"`
	ProjectID *string `json:"projectId"`
	Language  string  `json:"language"`
}
