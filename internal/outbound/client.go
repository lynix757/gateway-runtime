package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	mw "gateway-runtime/internal/httpx/middleware"
)

type AccessTokenSource interface {
	AccessToken(ctx context.Context, sessionID string) (string, error)
}

type Client struct {
	BaseURL      *url.URL
	HTTP         *http.Client
	Tokens       AccessTokenSource
	MaxRetries   int
	RetryBackoff time.Duration
	MaxBodyBytes int64
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("outbound base URL must be absolute")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		BaseURL:      u,
		HTTP:         &http.Client{Timeout: timeout},
		MaxRetries:   1,
		RetryBackoff: 50 * time.Millisecond,
		MaxBodyBytes: 1 << 20,
	}, nil
}

func (c *Client) DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}

	attempts := 1
	if isIdempotent(method) && c.MaxRetries > 0 {
		attempts += c.MaxRetries
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := c.newRequest(ctx, sessionID, method, path, body)
		if err != nil {
			return err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return fmt.Errorf("outbound request: %w", ctx.Err())
			}
			if attempt+1 < attempts {
				if err := waitBackoff(ctx, c.RetryBackoff); err != nil {
					return fmt.Errorf("outbound request: %w", err)
				}
				continue
			}
			return fmt.Errorf("outbound request: %w", err)
		}

		err = c.handleResponse(resp, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt+1 >= attempts || !retryableResponse(resp.StatusCode) {
			return err
		}
		if err := waitBackoff(ctx, c.RetryBackoff); err != nil {
			return err
		}
	}
	return lastErr
}

func (c *Client) newRequest(ctx context.Context, sessionID, method, route string, body []byte) (*http.Request, error) {
	u := *c.BaseURL
	u.Path = strings.TrimRight(c.BaseURL.Path, "/") + "/" + strings.TrimLeft(route, "/")

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := mw.RequestIDFromContext(ctx); id != "" {
		req.Header.Set("X-Request-ID", id)
	}
	if traceID := mw.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}
	if sessionID != "" && c.Tokens != nil {
		token, err := c.Tokens.AccessToken(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func (c *Client) handleResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()
	limit := c.MaxBodyBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("outbound response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			Kind:       normalizeStatus(resp.StatusCode),
			StatusCode: resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
		}
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode outbound response: %w", err)
	}
	return nil
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func retryableResponse(status int) bool {
	switch status {
	case 502, 503, 504:
		return true
	default:
		return false
	}
}

func waitBackoff(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
