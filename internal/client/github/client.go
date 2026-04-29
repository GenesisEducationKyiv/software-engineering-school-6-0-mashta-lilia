package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github-release-notifier/internal/model"
)

const maxRetries = 3

type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string
}

func NewClient(token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		token:      token,
		baseURL:    "https://api.github.com",
	}
}

func (c *Client) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name))
	resp, err := c.doRequest(ctx, url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (c *Client) GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, url.PathEscape(owner), url.PathEscape(name))
	resp, err := c.doRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release model.Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

func (c *Client) doRequest(ctx context.Context, url string) (*http.Response, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		resp.Body.Close()

		if attempt == maxRetries {
			return nil, fmt.Errorf("github rate limit exceeded after %d retries", maxRetries)
		}

		wait := parseRateLimitWait(resp.Header, attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	// Unreachable: the loop always returns via attempt == maxRetries or a non-429 status.
	// Kept for compiler satisfaction.
	return nil, fmt.Errorf("github rate limit exceeded")
}

// parseRateLimitWait determines how long to wait before retrying.
// It checks Retry-After first, then X-RateLimit-Reset, and falls back to exponential backoff.
func parseRateLimitWait(headers http.Header, attempt int) time.Duration {
	// 1. Retry-After header (seconds)
	if ra := headers.Get("Retry-After"); ra != "" {
		if seconds, err := strconv.Atoi(ra); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	// 2. X-RateLimit-Reset header (Unix timestamp)
	if reset := headers.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			wait := time.Until(time.Unix(ts, 0))
			if wait > 0 && wait < 120*time.Second {
				return wait
			}
		}
	}

	// 3. Exponential backoff: 1s, 2s, 4s
	return time.Duration(1<<uint(attempt)) * time.Second
}
