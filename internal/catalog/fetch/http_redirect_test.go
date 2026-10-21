package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Not parallel: it points the package's transport at a TLS test server
// and restores it, before the parallel tests build their clients.
func TestHTTPFetchFollowsOnlySameHostRedirects(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("version: 1\n")) })
	mux.HandleFunc("/same", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusFound) })
	mux.HandleFunc("/other", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://localhost"+strings.TrimPrefix(srv.URL, "https://127.0.0.1")+"/ok", http.StatusFound)
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+strings.TrimPrefix(srv.URL, "https://")+"/ok", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	old := transport
	transport = srv.Client().Transport
	t.Cleanup(func() { transport = old })

	fetch := HTTPFetch(context.Background())
	for path, wantErr := range map[string]string{"/ok": "", "/same": "", "/other": "refused", "/plain": "refused", "/loop": "too many redirects", "/missing": "HTTP 404"} {
		b, err := fetch(srv.URL + path)
		if wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Errorf("%s: want %q, got %v", path, wantErr, err)
			}
			continue
		}
		if err != nil || string(b) != "version: 1\n" {
			t.Errorf("%s: %q %v", path, b, err)
		}
	}
	if _, err := fetch("https://" + string([]byte{0x7f}) + "/x"); err == nil {
		t.Error("an invalid URL is an error")
	}
}
