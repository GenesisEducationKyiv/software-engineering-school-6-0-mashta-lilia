package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github-release-notifier/internal/model"
	"net/http"
	"net/url"
	"time"
)

const (
	maxRetries        = 3
	httpClientTimeout = 10 * time.Second
)

type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string
	retry      RetryStrategy
}

func NewClient(token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: httpClientTimeout},
		token:      token,
		baseURL:    "https://api.github.com",
		retry:      HeaderAwareRetry{},
	}
}

func (c *Client) RepoExists(ctx context.Context, owner, name string) (bool, error) {
	repoURL := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name))
	resp, err := c.doRequest(ctx, repoURL)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck // body close error is safe to ignore

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

func (c *Client) GetLatestRelease(ctx context.Context, owner, name string) (*model.Release, error) {
	releaseURL := fmt.Sprintf(
		"%s/repos/%s/%s/releases/latest",
		c.baseURL, url.PathEscape(owner), url.PathEscape(name),
	)
	resp, err := c.doRequest(ctx, releaseURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // body close error is safe to ignore

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

func (c *Client) doRequest(ctx context.Context, rawURL string) (*http.Response, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("building GitHub request: %w", err)
		}

		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("executing GitHub request: %w", err)
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		resp.Body.Close() //nolint:errcheck,gosec // discarding 429 response body before retry

		if attempt == maxRetries {
			return nil, fmt.Errorf("github rate limit exceeded after %d retries", maxRetries)
		}

		wait := c.retry.NextWait(resp.Header, attempt)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for rate limit reset: %w", ctx.Err())
		case <-time.After(wait):
		}
	}

	// Unreachable: the loop always returns via attempt == maxRetries or a non-429 status.
	// Kept for compiler satisfaction.
	return nil, errors.New("github rate limit exceeded")
}
