package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultTimeout is used for GET requests so calls don't hang in restricted environments.
const DefaultTimeout = 15 * time.Second

// Client wraps HTTP capabilities for CRE workflows.
// In the real CRE SDK, HTTP access is provided via runtime.HTTPClient().
// This is a simple wrapper until the real CRE HTTP capability is integrated.
type Client struct {
	inner *http.Client
}

// NewClient creates a new HTTP client with DefaultTimeout.
func NewClient() *Client {
	return &Client{
		inner: &http.Client{Timeout: DefaultTimeout},
	}
}

// Get performs an HTTP GET request and returns the response body.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	body, _, err := c.GetWithStatus(ctx, url)
	return body, err
}

// GetWithStatus performs an HTTP GET request and returns body, status code, and error.
// Callers can use statusCode to handle 4xx/5xx and still inspect the body.
func (c *Client) GetWithStatus(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.inner.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// Post performs an HTTP POST request.
func (c *Client) Post(ctx context.Context, url string, contentType string, body []byte) ([]byte, error) {
	// TODO: Implement
	return nil, nil
}
