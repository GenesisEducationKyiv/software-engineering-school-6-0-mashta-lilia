package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	restTimeout         = 30 * time.Second
	restMaxIdleConns    = 100
	restIdleConnTimeout = 90 * time.Second
	restErrBodyLimit    = 512
)

// restTransport pools connections so a service-to-service caller reuses TCP/TLS
// instead of paying a handshake per request (the default of 2 idle/host is too low).
func restTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	tr := base.Clone()
	tr.MaxIdleConns = restMaxIdleConns
	tr.MaxIdleConnsPerHost = restMaxIdleConns
	tr.IdleConnTimeout = restIdleConnTimeout
	return tr
}

// RESTClient calls the notifier's HTTP/JSON verify-email endpoint — the REST
// transport kept alongside gRPC for the HW10 comparison.
type RESTClient struct {
	baseURL string
	http    *http.Client
	log     *logger.Logger
}

func NewRESTClient(baseURL string, log *logger.Logger) (*RESTClient, error) {
	if baseURL == "" {
		return nil, errors.New("notification rest client: base url is empty")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &RESTClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: restTimeout, Transport: restTransport()},
		log:     log,
	}, nil
}

func (c *RESTClient) VerifyEmail(ctx context.Context, email, confirmURL, repo string) (bool, error) {
	body, err := json.Marshal(map[string]string{
		"email":       email,
		"confirm_url": confirmURL,
		"repo":        repo,
	})
	if err != nil {
		return false, fmt.Errorf("marshaling verify-email request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/api/v1/verify-email", bytes.NewReader(body),
	)
	if err != nil {
		return false, fmt.Errorf("building verify-email request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("notification rest verify email failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // body close error is safe to ignore

	if resp.StatusCode != http.StatusOK {
		limited := io.LimitReader(resp.Body, restErrBodyLimit)
		msg, _ := io.ReadAll(limited) //nolint:errcheck // best-effort error detail
		return false, fmt.Errorf("notification rest verify email: status=%d body=%s", resp.StatusCode, msg)
	}
	var out struct {
		Delivered bool `json:"delivered"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("decoding verify-email response: %w", err)
	}
	return out.Delivered, nil
}
