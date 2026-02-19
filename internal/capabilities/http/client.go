package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Client wraps HTTP capabilities for CRE workflows.
// In the real CRE SDK, HTTP access is provided via runtime.HTTPClient().
// This is a simple wrapper until the real CRE HTTP capability is integrated.
type Client struct {
	inner *http.Client
}

// NewClient creates a new HTTP client.
func NewClient() *Client {
	return &Client{
		inner: &http.Client{},
	}
}

// Get performs an HTTP GET request.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.inner.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return body, nil
}

// Post performs an HTTP POST request.
func (c *Client) Post(ctx context.Context, url string, contentType string, body []byte) ([]byte, error) {
	// TODO: Implement
	return nil, nil
}
