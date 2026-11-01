package transport_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/transport"
)

const queryCanary = "SENTINEL-QUERY-MUST-NOT-LEAK"

func TestErrorMessagesNameTheEndpointAndNeverCarryItsQuery(t *testing.T) {
	html := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}
	cases := []struct {
		name  string
		steps []http.HandlerFunc
		opts  []transport.Option
		raw   func(base string) string
		want  []string
	}{
		{
			name:  "content type",
			steps: []http.HandlerFunc{html},
			raw:   func(base string) string { return base + "/a?key=" + queryCanary },
			want:  []string{"/a:", "unexpected content type text/html"},
		},
		{
			name:  "response too large",
			steps: []http.HandlerFunc{ok(strings.Repeat("x", 100))},
			opts:  []transport.Option{transport.WithMaxBodyBytes(50)},
			raw:   func(base string) string { return base + "/a?key=" + queryCanary },
			want:  []string{"/a:", "response larger than the 50 byte limit"},
		},
		{
			name:  "retry after",
			steps: []http.HandlerFunc{status(429, "Retry-After", "3600")},
			raw:   func(base string) string { return base + "/a?key=" + queryCanary },
			want:  []string{"/a:", "server asked to wait 1h0m0s, above the"},
		},
		{
			name:  "retry exhausted",
			steps: []http.HandlerFunc{status(503)},
			opts:  []transport.Option{transport.WithMaxAttempts(2)},
			raw:   func(base string) string { return base + "/a?key=" + queryCanary },
			want:  []string{"/a:", "gave up after 2 attempts", "unexpected status 503"},
		},
		{
			name: "unsupported endpoint",
			raw:  func(string) string { return "ftp://ftp.example.org/a?key=" + queryCanary },
			want: []string{"ftp://ftp.example.org/a:", "only http and https endpoints are supported"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := ""
			if len(tc.steps) > 0 {
				srv, _ := serve(t, tc.steps...)
				base = srv.URL
			}
			c := newClient(t, newFakeClock(), tc.opts...)
			_, err := c.Get(context.Background(), endpointOf(t, tc.raw(base)), nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q lacks %q", msg, want)
				}
			}
			if strings.Contains(msg, queryCanary) || strings.Contains(msg, "key=") {
				t.Errorf("message leaks the query: %q", msg)
			}
		})
	}
}

func TestAbsentContentTypeIsNamedInTheMessage(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		_, _ = w.Write([]byte("<html></html>"))
	})
	c := newClient(t, newFakeClock())
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL+"/a?key="+queryCanary), nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected content type none") {
		t.Fatalf("error %v", err)
	}
}
