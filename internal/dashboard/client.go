package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
)

const maxStatusResponseBytes = 1 << 20

var defaultEndpoints = []string{
	"http://127.0.0.1:9190" + statusapi.Path,
	"http://127.0.0.1:9191" + statusapi.Path,
}

// Client fetches status snapshots from the local workload process.
type Client struct {
	httpClient *http.Client
	endpoints  []string
	selected   string
}

// NewClient validates an optional loopback endpoint. An empty endpoint enables
// auto-discovery of the server and client defaults.
func NewClient(endpoint string) (*Client, error) {
	endpoints := defaultEndpoints
	if endpoint != "" {
		normalized, err := normalizeEndpoint(endpoint)
		if err != nil {
			return nil, err
		}
		endpoints = []string{normalized}
	}
	return &Client{
		endpoints: endpoints,
		httpClient: &http.Client{
			Timeout: 750 * time.Millisecond,
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext,
				DisableCompression:    true,
				MaxIdleConns:          2,
				MaxIdleConnsPerHost:   1,
				IdleConnTimeout:       30 * time.Second,
				ResponseHeaderTimeout: 500 * time.Millisecond,
			},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("status redirects are not allowed")
			},
		},
	}, nil
}

// Fetch returns the first valid local snapshot and sticks to that endpoint.
func (c *Client) Fetch() (statusapi.Snapshot, error) {
	endpoints := c.endpoints
	if c.selected != "" {
		endpoints = []string{c.selected}
	}
	var lastErr error
	for _, endpoint := range endpoints {
		snapshot, err := c.fetch(endpoint)
		if err == nil {
			c.selected = endpoint
			return snapshot, nil
		}
		lastErr = err
	}
	if c.selected != "" {
		// Retry discovery if the process restarted on the other default port.
		c.selected = ""
	}
	if lastErr == nil {
		lastErr = errors.New("no local status endpoints configured")
	}
	return statusapi.Snapshot{}, lastErr
}

func (c *Client) fetch(endpoint string) (statusapi.Snapshot, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return statusapi.Snapshot{}, fmt.Errorf("create status request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return statusapi.Snapshot{}, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return statusapi.Snapshot{}, fmt.Errorf("fetch %s: status %s", endpoint, response.Status)
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		return statusapi.Snapshot{}, fmt.Errorf("fetch %s: unexpected content type %q", endpoint, contentType)
	}
	limited := io.LimitReader(response.Body, maxStatusResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return statusapi.Snapshot{}, fmt.Errorf("read status response: %w", err)
	}
	if len(payload) > maxStatusResponseBytes {
		return statusapi.Snapshot{}, errors.New("status response exceeds 1 MiB")
	}
	var snapshot statusapi.Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return statusapi.Snapshot{}, fmt.Errorf("decode status response: %w", err)
	}
	if snapshot.SchemaVersion != statusapi.SchemaVersion {
		return statusapi.Snapshot{}, fmt.Errorf("unsupported status schema %d", snapshot.SchemaVersion)
	}
	if snapshot.Role != "client" && snapshot.Role != "server" {
		return statusapi.Snapshot{}, fmt.Errorf("unsupported status role %q", snapshot.Role)
	}
	if snapshot.Role == "client" && snapshot.Client == nil {
		return statusapi.Snapshot{}, errors.New("client status response is missing client data")
	}
	if snapshot.Role == "server" && snapshot.Server == nil {
		return statusapi.Snapshot{}, errors.New("server status response is missing server data")
	}
	if snapshot.SampledAt.IsZero() || snapshot.StartedAt.IsZero() {
		return statusapi.Snapshot{}, errors.New("status response is missing timestamps")
	}
	return snapshot, nil
}

func normalizeEndpoint(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint: %w", err)
	}
	if parsed.Scheme != "http" {
		return "", errors.New("dashboard endpoint must use http on loopback")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("dashboard endpoint cannot contain credentials, query, or fragment")
	}
	host := parsed.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", errors.New("dashboard endpoint must use a loopback address")
		}
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("dashboard endpoint must include a valid port")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = statusapi.Path
	} else if parsed.Path != statusapi.Path {
		return "", fmt.Errorf("dashboard endpoint path must be %s", statusapi.Path)
	}
	if host == "localhost" {
		parsed.Host = net.JoinHostPort("127.0.0.1", parsed.Port())
	}
	return parsed.String(), nil
}
