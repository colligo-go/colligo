package transport

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/colligo-go/colligo/internal/buildinfo"
	"github.com/colligo-go/colligo/logf"
	"github.com/colligo-go/colligo/secret"
)

const (
	defaultMaxAttempts    = 4
	defaultAttemptTimeout = 30 * time.Second
	defaultBackoffBase    = time.Second
	defaultBackoffCap     = 300 * time.Second
	defaultMaxBodyBytes   = 16 << 20
	defaultFallbackRate   = 2
	errorBodyDrainBytes   = 4 << 10
	jitterDivisor         = 4
	maxRetryAfterSeconds  = 1 << 31
)

var defaultRates = map[string]int{
	"ebi.ac.uk":          4,
	"ftp.ensembl.org":    2,
	"ncbi.nlm.nih.gov":   3,
	"www.cbioportal.org": 2,
	"files.rcsb.org":     4,
	"dfam.org":           2,
}

var defaultContentTypes = []string{
	"text/plain",
	"text/tab-separated-values",
	"text/csv",
	"application/json",
	"application/x-ndjson",
	"application/xml",
	"text/xml",
}

// Client is a Transport for metadata: GET only, rate limited per domain,
// retrying what is transient, bounded in time and in size. It is safe for
// concurrent use.
type Client struct {
	http           *http.Client
	userAgent      string
	maxAttempts    int
	attemptTimeout time.Duration
	backoffBase    time.Duration
	backoffCap     time.Duration
	maxBody        int64
	allowed        map[string]bool
	rates          map[string]int
	fallbackRate   int
	clock          Clock
	jitter         func(time.Duration) time.Duration
	logger         Logger
	spacing        *spacer
}

var _ Transport = (*Client)(nil)

func New(opts ...Option) (*Client, error) {
	c := &Client{
		http:           &http.Client{},
		userAgent:      "colligo/" + buildinfo.Version(),
		maxAttempts:    defaultMaxAttempts,
		attemptTimeout: defaultAttemptTimeout,
		backoffBase:    defaultBackoffBase,
		backoffCap:     defaultBackoffCap,
		maxBody:        defaultMaxBodyBytes,
		allowed:        map[string]bool{},
		rates:          map[string]int{},
		fallbackRate:   defaultFallbackRate,
		clock:          systemClock{},
		jitter:         boundedJitter,
		logger:         silentLogger{},
		spacing:        newSpacer(),
	}
	for domain, perSecond := range defaultRates {
		c.rates[domain] = perSecond
	}
	for _, t := range defaultContentTypes {
		c.allowed[t] = true
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) Get(ctx context.Context, endpoint secret.Endpoint, rng *ByteRange) (*Response, error) {
	if rng != nil {
		return nil, &RangeNotImplementedError{}
	}
	target, err := url.Parse(endpoint.Reveal())
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return nil, &UnsupportedEndpointError{Endpoint: endpoint}
	}
	domain, perSecond := c.rateFor(strings.ToLower(target.Hostname()))
	interval := time.Second / time.Duration(perSecond)

	var last error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if err := c.awaitSlot(ctx, domain, interval); err != nil {
			return nil, err
		}
		resp, fail := c.attempt(ctx, endpoint, target)
		if fail == nil {
			return resp, nil
		}
		if !fail.retryable {
			return nil, fail.err
		}
		if fail.retryAfter > c.backoffCap {
			return nil, &RetryAfterError{Endpoint: endpoint, After: fail.retryAfter, Cap: c.backoffCap}
		}
		last = fail.err
		if attempt == c.maxAttempts-1 {
			break
		}
		delay := max(c.backoff(attempt), fail.retryAfter)
		c.logger.Warn("metadata request will be retried",
			logf.Endpoint("url", endpoint),
			logf.Str("delay", delay.String()),
			logf.Err(last))
		if err := c.clock.Sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, &RetryExhaustedError{Endpoint: endpoint, Attempts: c.maxAttempts, Err: last}
}

type failure struct {
	err        error
	retryable  bool
	retryAfter time.Duration
}

func (c *Client) attempt(ctx context.Context, endpoint secret.Endpoint, target *url.URL) (*Response, *failure) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		cancel()
		return nil, &failure{err: &UnsupportedEndpointError{Endpoint: endpoint}}
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, &failure{err: ctx.Err()}
		}
		return nil, &failure{err: withoutURL(err), retryable: true}
	}

	status := resp.StatusCode
	switch {
	case status >= 200 && status < 300:
		accepted, fail := c.accept(endpoint, resp, cancel)
		return accepted, fail
	case status == http.StatusNotFound || status == http.StatusGone:
		drain(resp, cancel)
		return nil, &failure{err: &NotFoundError{Endpoint: endpoint, Status: status}}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		drain(resp, cancel)
		c.logger.Warn("metadata source refused access", logf.Endpoint("url", endpoint), logf.Str("status", strconv.Itoa(status)))
		return nil, &failure{err: &AuthError{Endpoint: endpoint, Status: status}}
	case status == http.StatusTooManyRequests || status >= 500:
		retryAfter := c.parseRetryAfter(resp.Header.Get("Retry-After"))
		contentType := mediaType(resp.Header.Get("Content-Type"))
		drain(resp, cancel)
		return nil, &failure{
			err:        &StatusError{Endpoint: endpoint, Status: status, ContentType: contentType},
			retryable:  true,
			retryAfter: retryAfter,
		}
	default:
		contentType := mediaType(resp.Header.Get("Content-Type"))
		drain(resp, cancel)
		return nil, &failure{err: &StatusError{Endpoint: endpoint, Status: status, ContentType: contentType}}
	}
}

func (c *Client) accept(endpoint secret.Endpoint, resp *http.Response, cancel context.CancelFunc) (*Response, *failure) {
	contentType := mediaType(resp.Header.Get("Content-Type"))
	if !c.allowed[contentType] {
		drain(resp, cancel)
		return nil, &failure{err: &ContentTypeError{Endpoint: endpoint, Got: contentType}}
	}
	tooLarge := &ResponseTooLargeError{Endpoint: endpoint, Limit: c.maxBody}
	if resp.ContentLength > c.maxBody {
		drain(resp, cancel)
		return nil, &failure{err: tooLarge}
	}
	body := &boundedBody{rc: resp.Body, limit: c.maxBody, exceeded: tooLarge, cancel: cancel}
	return &Response{
		Status:        resp.StatusCode,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
		Body:          body,
	}, nil
}

func (c *Client) awaitSlot(ctx context.Context, domain string, interval time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := c.clock.Now()
	slot := c.spacing.reserve(domain, interval, now)
	if wait := slot.Sub(now); wait > 0 {
		return c.clock.Sleep(ctx, wait)
	}
	return nil
}

func (c *Client) rateFor(host string) (string, int) {
	matched, rate := "", c.fallbackRate
	for domain, perSecond := range c.rates {
		if (host == domain || strings.HasSuffix(host, "."+domain)) && len(domain) > len(matched) {
			matched, rate = domain, perSecond
		}
	}
	if matched == "" {
		return host, rate
	}
	return matched, rate
}

func (c *Client) backoff(attempt int) time.Duration {
	delay := c.backoffBase
	for i := 0; i < attempt && delay < c.backoffCap; i++ {
		delay *= 2
	}
	delay = min(delay, c.backoffCap)
	return delay + c.jitter(delay)
}

func (c *Client) parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(min(seconds, maxRetryAfterSeconds)) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(date.Sub(c.clock.Now()), 0)
	}
	return 0
}

func boundedJitter(delay time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(delay)/jitterDivisor + 1))
}

func mediaType(header string) string {
	parsed, _, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return parsed
}

func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func drain(resp *http.Response, cancel context.CancelFunc) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyDrainBytes))
	_ = resp.Body.Close()
	cancel()
}

type boundedBody struct {
	rc       io.ReadCloser
	limit    int64
	read     int64
	exceeded error
	cancel   context.CancelFunc
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.read > b.limit {
		return 0, b.exceeded
	}
	if room := b.limit - b.read + 1; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := b.rc.Read(p)
	b.read += int64(n)
	if b.read > b.limit {
		return n - int(b.read-b.limit), b.exceeded
	}
	return n, err
}

func (b *boundedBody) Close() error {
	err := b.rc.Close()
	b.cancel()
	return err
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type silentLogger struct{}

func (silentLogger) Debug(string, ...logf.Field) {}
func (silentLogger) Warn(string, ...logf.Field)  {}
