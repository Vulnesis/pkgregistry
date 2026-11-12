package pkgregistry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpDoer is the subset of *http.Client the registry uses, so tests (and host
// services) can inject their own transport.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// maxResponseBytes caps a single registry response. npm packuments for popular
// packages can be several MB; 64 MiB is a generous ceiling that still guards
// against a pathological or hostile response.
const maxResponseBytes = 64 << 20

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// get performs a GET with up to three attempts (retrying transport errors and
// 5xx with a short linear backoff). It returns ErrNotFound for 404/410.
func (r *Registry) get(ctx context.Context, url, accept string) ([]byte, error) {
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		req.Header.Set("User-Agent", r.userAgent)

		resp, err := r.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			return nil, ErrNotFound
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if readErr != nil {
				return nil, readErr
			}
			return body, nil
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("pkgregistry: GET %s: status %d", url, resp.StatusCode)
			continue
		default:
			return nil, fmt.Errorf("pkgregistry: GET %s: status %d", url, resp.StatusCode)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("pkgregistry: GET %s: exhausted retries", url)
	}
	return nil, lastErr
}
