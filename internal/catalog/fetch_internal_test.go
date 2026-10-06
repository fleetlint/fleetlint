package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadBounded(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("version: 1\n")) })
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/limit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, maxCatalogLen)) })
	mux.HandleFunc("/huge", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, maxCatalogLen+1)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cases := []struct {
		path    string
		wantLen int
		wantErr string
	}{
		{path: "/ok", wantLen: len("version: 1\n")},
		{path: "/limit", wantLen: maxCatalogLen},
		{path: "/missing", wantErr: "HTTP 404"},
		{path: "/huge", wantErr: "larger than"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+c.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close() //nolint:errcheck // test read side
			body, err := readBounded(srv.URL+c.path, resp)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), c.path) {
					t.Fatalf("want error naming %q and %q, got %v", c.wantErr, c.path, err)
				}
				return
			}
			if err != nil || len(body) != c.wantLen {
				t.Fatalf("len=%d err=%v, want %d", len(body), err, c.wantLen)
			}
		})
	}
}

func TestHTTPFetchTransportLimits(t *testing.T) {
	t.Parallel()
	fetch := HTTPFetch(context.Background())
	if _, err := fetch("http://example.invalid/catalog.yaml"); err == nil || !strings.Contains(err.Error(), "only https") {
		t.Fatalf("plain http must be refused, got %v", err)
	}
	// A cancelled context fails before any connection is attempted.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := HTTPFetch(ctx)("https://127.0.0.1:1/catalog.yaml"); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancelled context must abort the fetch, got %v", err)
	}
}
