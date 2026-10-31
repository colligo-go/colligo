package transport_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colligo-go/colligo/logf"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
	"github.com/colligo-go/colligo/transport"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0).UTC()} }

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	f.now = f.now.Add(d)
	return nil
}

func (f *fakeClock) recorded() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

type recordingLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *recordingLogger) Debug(msg string, fields ...logf.Field) { l.add(msg, fields) }
func (l *recordingLogger) Warn(msg string, fields ...logf.Field)  { l.add(msg, fields) }

func (l *recordingLogger) add(msg string, fields []logf.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	parts := []string{msg}
	for _, f := range fields {
		parts = append(parts, f.String())
	}
	l.entries = append(l.entries, strings.Join(parts, " "))
}

func (l *recordingLogger) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.entries, "\n")
}

func noJitter(time.Duration) time.Duration { return 0 }

func plainText(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
	_, _ = io.WriteString(w, body)
}

func serve(t *testing.T, steps ...http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(calls.Add(1)) - 1
		steps[min(i, len(steps)-1)](w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func endpointOf(t *testing.T, raw string) secret.Endpoint {
	t.Helper()
	e, err := secret.NewEndpoint(raw)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func newClient(t *testing.T, clock *fakeClock, opts ...transport.Option) *transport.Client {
	t.Helper()
	base := []transport.Option{
		transport.WithClock(clock),
		transport.WithJitter(noJitter),
		transport.WithDefaultRate(1000),
	}
	c, err := transport.New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func readAll(t *testing.T, resp *transport.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func status(code int, header ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		w.WriteHeader(code)
	}
}

func ok(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { plainText(w, body) }
}

func TestGetReturnsTheBody(t *testing.T) {
	srv, _ := serve(t, ok("run_accession\nDRR021374\n"))
	c := newClient(t, newFakeClock())
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL+"/filereport"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || !strings.HasPrefix(resp.ContentType, "text/plain") {
		t.Fatalf("status %d type %q", resp.Status, resp.ContentType)
	}
	if got := readAll(t, resp); got != "run_accession\nDRR021374\n" {
		t.Fatalf("body %q", got)
	}
}

func TestGetSendsAUserAgentNamingColligo(t *testing.T) {
	var agent atomic.Value
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		agent.Store(r.Header.Get("User-Agent"))
		plainText(w, "x")
	})
	c := newClient(t, newFakeClock())
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if got, _ := agent.Load().(string); !strings.HasPrefix(got, "colligo/") || len(got) == len("colligo/") {
		t.Fatalf("user agent %q", got)
	}
}

func TestRetryAfterOnTooManyRequestsIsHonoured(t *testing.T) {
	srv, calls := serve(t, status(429, "Retry-After", "7"), ok("fine"))
	clock := newFakeClock()
	c := newClient(t, clock)
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if readAll(t, resp) != "fine" || calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
	if got := clock.recorded(); len(got) != 1 || got[0] != 7*time.Second {
		t.Fatalf("sleeps %v", got)
	}
}

func TestRetryAfterAsHTTPDateIsHonoured(t *testing.T) {
	clock := newFakeClock()
	date := clock.Now().Add(12 * time.Second).UTC().Format(http.TimeFormat)
	srv, _ := serve(t, status(503, "Retry-After", date), ok("fine"))
	c := newClient(t, clock)
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if got := clock.recorded(); len(got) != 1 || got[0] != 12*time.Second {
		t.Fatalf("sleeps %v", got)
	}
}

func TestBackoffWinsWhenRetryAfterIsSmaller(t *testing.T) {
	srv, _ := serve(t, status(503, "Retry-After", "0"), ok("fine"))
	clock := newFakeClock()
	c := newClient(t, clock)
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if got := clock.recorded(); len(got) != 1 || got[0] != time.Second {
		t.Fatalf("sleeps %v", got)
	}
}

func TestRetryAfterAboveTheCapStopsWithoutWaiting(t *testing.T) {
	srv, calls := serve(t, status(429, "Retry-After", "3600"), ok("never"))
	clock := newFakeClock()
	c := newClient(t, clock)
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	var tooLong *transport.RetryAfterError
	if !errors.As(err, &tooLong) || tooLong.After != time.Hour {
		t.Fatalf("error %v", err)
	}
	if calls.Load() != 1 || len(clock.recorded()) != 0 {
		t.Fatalf("calls %d sleeps %v", calls.Load(), clock.recorded())
	}
}

func TestServiceUnavailableRetriesThenSucceeds(t *testing.T) {
	srv, calls := serve(t, status(503), ok("recovered"))
	clock := newFakeClock()
	c := newClient(t, clock)
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if readAll(t, resp) != "recovered" || calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
	if got := clock.recorded(); len(got) != 1 || got[0] != time.Second {
		t.Fatalf("sleeps %v", got)
	}
}

func TestBackoffDoublesUpToTheCap(t *testing.T) {
	srv, calls := serve(t, status(500))
	clock := newFakeClock()
	c := newClient(t, clock, transport.WithMaxAttempts(5), transport.WithBackoff(time.Second, 3*time.Second))
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	var exhausted *transport.RetryExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Attempts != 5 {
		t.Fatalf("error %v", err)
	}
	var last *transport.StatusError
	if !errors.As(err, &last) || last.Status != 500 {
		t.Fatalf("last error %v", err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}
	got := clock.recorded()
	if calls.Load() != 5 || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("calls %d sleeps %v", calls.Load(), got)
	}
}

func TestJitterIsAddedAndStaysBounded(t *testing.T) {
	srv, _ := serve(t, status(502), ok("fine"))
	clock := newFakeClock()
	c, err := transport.New(
		transport.WithClock(clock),
		transport.WithDefaultRate(1000),
		transport.WithJitter(func(d time.Duration) time.Duration { return d / 4 }),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if got := clock.recorded(); len(got) != 1 || got[0] != 1250*time.Millisecond {
		t.Fatalf("sleeps %v", got)
	}
}

func TestDefaultJitterNeverExceedsAQuarterOfTheDelay(t *testing.T) {
	clock := newFakeClock()
	srv, _ := serve(t, status(500))
	c, err := transport.New(transport.WithClock(clock), transport.WithDefaultRate(1000), transport.WithMaxAttempts(40))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	delay := time.Second
	for i, got := range clock.recorded() {
		if got < delay || got > delay+delay/4 {
			t.Fatalf("sleep %d is %v, want within [%v, %v]", i, got, delay, delay+delay/4)
		}
		delay = min(delay*2, 300*time.Second)
	}
}

func TestPermanentStatusesAreNotRetried(t *testing.T) {
	cases := []struct {
		code int
		want func(error) bool
	}{
		{404, func(err error) bool { var e *transport.NotFoundError; return errors.As(err, &e) }},
		{410, func(err error) bool { var e *transport.NotFoundError; return errors.As(err, &e) }},
		{401, func(err error) bool { var e *transport.AuthError; return errors.As(err, &e) }},
		{403, func(err error) bool { var e *transport.AuthError; return errors.As(err, &e) }},
		{400, func(err error) bool { var e *transport.StatusError; return errors.As(err, &e) && e.Status == 400 }},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			srv, calls := serve(t, status(tc.code), ok("never"))
			clock := newFakeClock()
			c := newClient(t, clock)
			_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
			if !tc.want(err) {
				t.Fatalf("error %v", err)
			}
			if calls.Load() != 1 || len(clock.recorded()) != 0 {
				t.Fatalf("calls %d sleeps %v", calls.Load(), clock.recorded())
			}
		})
	}
}

func TestAttemptTimeoutCancelsTheRequest(t *testing.T) {
	released := make(chan struct{})
	srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(released)
	})
	c := newClient(t, newFakeClock(), transport.WithAttemptTimeout(50*time.Millisecond), transport.WithMaxAttempts(1))
	started := time.Now()
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v", err)
	}
	if time.Since(started) > 5*time.Second || calls.Load() != 1 {
		t.Fatalf("took %v with %d calls", time.Since(started), calls.Load())
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("server handler never saw the cancellation")
	}
}

func TestTimeoutIsRetried(t *testing.T) {
	srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, ok("second try"))
	c := newClient(t, newFakeClock(), transport.WithAttemptTimeout(50*time.Millisecond))
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if readAll(t, resp) != "second try" || calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestCancelledCallerStopsWithoutRetry(t *testing.T) {
	srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	c := newClient(t, newFakeClock())
	_, err := c.Get(ctx, endpointOf(t, srv.URL), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestDeclaredSizeAboveTheLimitIsRefusedBeforeReading(t *testing.T) {
	srv, _ := serve(t, ok(strings.Repeat("x", 100)))
	c := newClient(t, newFakeClock(), transport.WithMaxBodyBytes(50))
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	var large *transport.ResponseTooLargeError
	if !errors.As(err, &large) || large.Limit != 50 {
		t.Fatalf("error %v", err)
	}
}

func TestStreamedBodyAboveTheLimitFailsTheRead(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		flusher := w.(http.Flusher)
		for range 10 {
			_, _ = io.WriteString(w, strings.Repeat("x", 10))
			flusher.Flush()
		}
	})
	c := newClient(t, newFakeClock(), transport.WithMaxBodyBytes(50))
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	var large *transport.ResponseTooLargeError
	if !errors.As(err, &large) {
		t.Fatalf("error %v", err)
	}
	if len(data) != 50 {
		t.Fatalf("read %d bytes before refusing, want exactly the limit", len(data))
	}
}

func TestBodyExactlyAtTheLimitPasses(t *testing.T) {
	srv, _ := serve(t, ok(strings.Repeat("x", 50)))
	c := newClient(t, newFakeClock(), transport.WithMaxBodyBytes(50))
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(readAll(t, resp)) != 50 {
		t.Fatal("short body")
	}
}

func TestHTMLWhereTextWasExpectedIsFlagged(t *testing.T) {
	cases := map[string]string{
		"html":   "text/html; charset=utf-8",
		"xhtml":  "application/xhtml+xml",
		"absent": "",
	}
	for name, contentType := range cases {
		t.Run(name, func(t *testing.T) {
			srv, calls := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				if contentType == "" {
					w.Header()["Content-Type"] = nil
				} else {
					w.Header().Set("Content-Type", contentType)
				}
				_, _ = io.WriteString(w, "<html><body>maintenance</body></html>")
			})
			c := newClient(t, newFakeClock())
			_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
			var wrong *transport.ContentTypeError
			if !errors.As(err, &wrong) {
				t.Fatalf("error %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls %d", calls.Load())
			}
		})
	}
}

func TestContentTypesCanBeReplaced(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<p>")
	})
	c := newClient(t, newFakeClock(), transport.WithContentTypes("text/html"))
	resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
}

func TestHTMLErrorPageBehindA500IsReportedAsContentType(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "<html>Internal Server Error</html>")
	})
	c := newClient(t, newFakeClock(), transport.WithMaxAttempts(2))
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
	var status *transport.StatusError
	if !errors.As(err, &status) || status.Status != 500 || status.ContentType != "text/html" {
		t.Fatalf("error %v", err)
	}
}

func TestRangeIsRefusedWithoutARequest(t *testing.T) {
	srv, calls := serve(t, ok("never"))
	c := newClient(t, newFakeClock())
	_, err := c.Get(context.Background(), endpointOf(t, srv.URL), &transport.ByteRange{Start: 0, End: 9})
	var refused *transport.RangeNotImplementedError
	if !errors.As(err, &refused) || calls.Load() != 0 {
		t.Fatalf("error %v calls %d", err, calls.Load())
	}
}

func TestNonHTTPEndpointsAreRefused(t *testing.T) {
	c := newClient(t, newFakeClock())
	for _, raw := range []string{"ftp://ftp.sra.ebi.ac.uk/vol1/x.fastq.gz", "file:///etc/hosts", "/relative/path", "http://"} {
		_, err := c.Get(context.Background(), endpointOf(t, raw), nil)
		var refused *transport.UnsupportedEndpointError
		if !errors.As(err, &refused) {
			t.Errorf("%s: error %v", raw, err)
		}
	}
}

func TestErrorsAndLogsNeverCarryTheQueryString(t *testing.T) {
	logger := &recordingLogger{}
	srv, _ := serve(t, status(503), status(404))
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	cases := map[string]string{
		"status":  srv.URL + "/a?api_key=SENTINEL-QUERY",
		"network": closedURL + "/a?api_key=SENTINEL-QUERY",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			c := newClient(t, newFakeClock(), transport.WithLogger(logger), transport.WithMaxAttempts(2))
			_, err := c.Get(context.Background(), endpointOf(t, raw), nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "SENTINEL-QUERY") {
				t.Fatalf("error leaks the query: %v", err)
			}
		})
	}
	if strings.Contains(logger.text(), "SENTINEL-QUERY") || !strings.Contains(logger.text(), "retried") {
		t.Fatalf("log: %q", logger.text())
	}
}

func TestEveryErrorCarriesASeverity(t *testing.T) {
	errs := []interface{ Severity() model.Severity }{
		&transport.NotFoundError{}, &transport.AuthError{}, &transport.StatusError{},
		&transport.ContentTypeError{}, &transport.ResponseTooLargeError{}, &transport.RetryAfterError{},
		&transport.RetryExhaustedError{}, &transport.RangeNotImplementedError{}, &transport.UnsupportedEndpointError{},
	}
	for _, e := range errs {
		if e.Severity() == "" {
			t.Errorf("%T has no severity", e)
		}
	}
}

func TestRateLimitDelaysRequestsToTheSameDomain(t *testing.T) {
	srv, _ := serve(t, ok("x"))
	clock := newFakeClock()
	c, err := transport.New(transport.WithClock(clock), transport.WithJitter(noJitter), transport.WithDefaultRate(4))
	if err != nil {
		t.Fatal(err)
	}
	started := clock.Now()
	for range 9 {
		resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, resp)
	}
	if elapsed := clock.Now().Sub(started); elapsed != 2*time.Second {
		t.Fatalf("9 requests at 4 per second took %v of virtual time, want 2s", elapsed)
	}
}

func TestRateFollowsTheLongestMatchingDomain(t *testing.T) {
	srv, _ := serve(t, ok("x"))
	clock := newFakeClock()
	c, err := transport.New(
		transport.WithClock(clock), transport.WithJitter(noJitter),
		transport.WithDefaultRate(1), transport.WithRate("127.0.0.1", 10),
	)
	if err != nil {
		t.Fatal(err)
	}
	started := clock.Now()
	for range 6 {
		resp, err := c.Get(context.Background(), endpointOf(t, srv.URL), nil)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, resp)
	}
	if elapsed := clock.Now().Sub(started); elapsed != 500*time.Millisecond {
		t.Fatalf("elapsed %v, want 500ms at 10 per second", elapsed)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answerEveryHost(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Body:          io.NopCloser(strings.NewReader("x")),
		ContentLength: 1,
		Request:       r,
	}, nil
}

func TestHostsOfOneDomainShareItsCeiling(t *testing.T) {
	clock := newFakeClock()
	c := newClient(t, clock,
		transport.WithHTTPClient(&http.Client{Transport: roundTripFunc(answerEveryHost)}),
		transport.WithRate("example.org", 4),
	)
	started := clock.Now()
	for i := range 8 {
		host := []string{"www.example.org", "ftp.sra.example.org"}[i%2]
		resp, err := c.Get(context.Background(), endpointOf(t, "https://"+host+"/report"), nil)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, resp)
	}
	if elapsed := clock.Now().Sub(started); elapsed != 1750*time.Millisecond {
		t.Fatalf("8 requests over two hosts of one domain at 4 per second took %v, want 1.75s", elapsed)
	}
}

func TestUnlistedHostsKeepSeparateCeilings(t *testing.T) {
	clock := newFakeClock()
	c := newClient(t, clock,
		transport.WithHTTPClient(&http.Client{Transport: roundTripFunc(answerEveryHost)}),
		transport.WithDefaultRate(1),
	)
	started := clock.Now()
	for _, host := range []string{"a.example", "b.example", "c.example"} {
		resp, err := c.Get(context.Background(), endpointOf(t, "https://"+host+"/"), nil)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, resp)
	}
	if elapsed := clock.Now().Sub(started); elapsed != 0 {
		t.Fatalf("one request to each of three unlisted hosts waited %v, want none", elapsed)
	}
}

func TestConcurrentCallersStayUnderTheDomainCeiling(t *testing.T) {
	const perSecond = 100
	const callers = 12
	const each = 5
	srv, _ := serve(t, ok("x"))
	c, err := transport.New(transport.WithDefaultRate(perSecond), transport.WithJitter(noJitter))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := endpointOf(t, srv.URL)

	started := time.Now()
	var wg sync.WaitGroup
	var failures atomic.Int32
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				resp, err := c.Get(context.Background(), endpoint, nil)
				if err != nil {
					failures.Add(1)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("%d callers failed", failures.Load())
	}
	floor := time.Duration(callers*each-1) * (time.Second / perSecond)
	if elapsed := time.Since(started); elapsed < floor {
		t.Fatalf("%d requests finished in %v, the ceiling of %d per second needs at least %v", callers*each, elapsed, perSecond, floor)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	bad := map[string]transport.Option{
		"rate":       transport.WithRate("ebi.ac.uk", 0),
		"domain":     transport.WithRate("", 4),
		"default":    transport.WithDefaultRate(0),
		"attempts":   transport.WithMaxAttempts(0),
		"timeout":    transport.WithAttemptTimeout(0),
		"backoff":    transport.WithBackoff(2*time.Second, time.Second),
		"body":       transport.WithMaxBodyBytes(0),
		"nil jitter": transport.WithJitter(nil),
		"nil clock":  transport.WithClock(nil),
		"nil client": transport.WithHTTPClient(nil),
		"nil logger": transport.WithLogger(nil),
	}
	for name, opt := range bad {
		if _, err := transport.New(opt); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
