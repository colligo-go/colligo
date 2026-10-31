package transport

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/colligo-go/colligo/logf"
)

type Option func(*Client) error

// Clock lets a test drive time: rate limiting, backoff and Retry-After dates
// all read it, so none of them needs a real sleep.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Logger is the subset of the SPEC §9 Logger that the client uses.
type Logger interface {
	Debug(msg string, fields ...logf.Field)
	Warn(msg string, fields ...logf.Field)
}

// WithRate sets the ceiling of requests per second for a domain and its
// subdomains. It overrides the default of the same domain.
func WithRate(domain string, perSecond int) Option {
	return func(c *Client) error {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" || perSecond < 1 {
			return fmt.Errorf("transport: rate needs a domain and at least 1 request per second")
		}
		c.rates[domain] = perSecond
		return nil
	}
}

func WithDefaultRate(perSecond int) Option {
	return func(c *Client) error {
		if perSecond < 1 {
			return fmt.Errorf("transport: default rate must be at least 1 request per second")
		}
		c.fallbackRate = perSecond
		return nil
	}
}

func WithMaxAttempts(n int) Option {
	return func(c *Client) error {
		if n < 1 {
			return fmt.Errorf("transport: at least 1 attempt is required")
		}
		c.maxAttempts = n
		return nil
	}
}

func WithAttemptTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d <= 0 {
			return fmt.Errorf("transport: attempt timeout must be positive")
		}
		c.attemptTimeout = d
		return nil
	}
}

// WithBackoff sets the first delay and the cap of the exponential backoff.
func WithBackoff(base, limit time.Duration) Option {
	return func(c *Client) error {
		if base <= 0 || limit < base {
			return fmt.Errorf("transport: backoff needs 0 < base <= cap")
		}
		c.backoffBase = base
		c.backoffCap = limit
		return nil
	}
}

// WithJitter replaces the jitter added to each backoff delay. The function
// receives the delay and returns the extra time, which must stay bounded.
func WithJitter(jitter func(delay time.Duration) time.Duration) Option {
	return func(c *Client) error {
		if jitter == nil {
			return fmt.Errorf("transport: jitter function is required")
		}
		c.jitter = jitter
		return nil
	}
}

func WithClock(clock Clock) Option {
	return func(c *Client) error {
		if clock == nil {
			return fmt.Errorf("transport: clock is required")
		}
		c.clock = clock
		return nil
	}
}

func WithMaxBodyBytes(n int64) Option {
	return func(c *Client) error {
		if n < 1 {
			return fmt.Errorf("transport: body limit must be positive")
		}
		c.maxBody = n
		return nil
	}
}

// WithContentTypes replaces the media types accepted on a 2xx answer.
func WithContentTypes(types ...string) Option {
	return func(c *Client) error {
		allowed := make(map[string]bool, len(types))
		for _, t := range types {
			allowed[strings.ToLower(strings.TrimSpace(t))] = true
		}
		c.allowed = allowed
		return nil
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) error {
		if client == nil {
			return fmt.Errorf("transport: http client is required")
		}
		c.http = client
		return nil
	}
}

func WithLogger(logger Logger) Option {
	return func(c *Client) error {
		if logger == nil {
			return fmt.Errorf("transport: logger is required")
		}
		c.logger = logger
		return nil
	}
}
