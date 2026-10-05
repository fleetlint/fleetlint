package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	fetchTimeout  = 20 * time.Second
	maxCatalogLen = 2 << 20 // 2 MiB is far above any sane catalog
)

// HTTPFetch returns a Fetch function for Loader: https only, bounded in time
// and size, no redirects to other hosts. The digest check in loadRemote is
// what makes the content trustworthy; this only makes the transport sane.
func HTTPFetch(ctx context.Context) func(url string) ([]byte, error) {
	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect to %s refused", req.URL)
			}
			return nil
		},
	}
	return func(url string) ([]byte, error) {
		if !strings.HasPrefix(url, "https://") {
			return nil, fmt.Errorf("%s: only https catalogs are fetched", url)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "fleetlint")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close() //nolint:errcheck // read side; nothing to do on close failure
		return readBounded(url, resp)
	}
}

func readBounded(url string, resp *http.Response) ([]byte, error) {
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogLen+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxCatalogLen {
		return nil, fmt.Errorf("%s: catalog larger than %d bytes", url, maxCatalogLen)
	}
	return body, nil
}
