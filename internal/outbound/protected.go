package outbound

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"gateway-runtime/internal/observability"
)

type ProtectedClient struct {
	Next             JSONDoer
	MaxConcurrent    int
	FailureThreshold int
	OpenFor          time.Duration
	Now              func() time.Time
	ServiceName      string
	Metrics          *observability.Metrics

	once sync.Once
	sem  chan struct{}

	mu       sync.Mutex
	failures int
	openedAt time.Time
	halfOpen bool
}

func (c *ProtectedClient) DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error {
	if c.Next == nil {
		return &Error{Kind: ErrUnavailable, StatusCode: http.StatusServiceUnavailable, Message: "outbound client unavailable"}
	}
	c.init()

	acquired := false
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			acquired = true
		default:
			if c.Metrics != nil {
				c.Metrics.RecordOutboundRejected(c.ServiceName, "bulkhead")
			}
			return &Error{Kind: ErrUnavailable, StatusCode: http.StatusServiceUnavailable, Message: "bulkhead full"}
		}
	}
	if acquired {
		defer func() { <-c.sem }()
	}

	if err := c.beforeRequest(); err != nil {
		if c.Metrics != nil {
			c.Metrics.RecordOutboundRejected(c.ServiceName, "circuit")
		}
		return err
	}

	err := c.Next.DoJSON(ctx, sessionID, method, path, in, out)
	c.afterResult(err)
	return err
}

func (c *ProtectedClient) init() {
	c.once.Do(func() {
		if c.MaxConcurrent > 0 {
			c.sem = make(chan struct{}, c.MaxConcurrent)
		}
		if c.FailureThreshold <= 0 {
			c.FailureThreshold = 5
		}
		if c.OpenFor <= 0 {
			c.OpenFor = 15 * time.Second
		}
		if c.Now == nil {
			c.Now = time.Now
		}
	})
}

func (c *ProtectedClient) beforeRequest() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.openedAt.IsZero() {
		return nil
	}
	now := c.Now()
	if now.Sub(c.openedAt) < c.OpenFor {
		return &Error{Kind: ErrUnavailable, StatusCode: http.StatusServiceUnavailable, Message: "circuit open"}
	}
	if c.halfOpen {
		return &Error{Kind: ErrUnavailable, StatusCode: http.StatusServiceUnavailable, Message: "circuit half-open"}
	}
	c.halfOpen = true
	return nil
}

func (c *ProtectedClient) afterResult(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !isCircuitFailure(err) {
		c.failures = 0
		c.openedAt = time.Time{}
		c.halfOpen = false
		return
	}

	if c.halfOpen {
		c.openedAt = c.Now()
		c.halfOpen = false
		c.failures = c.FailureThreshold
		return
	}

	c.failures++
	if c.failures >= c.FailureThreshold {
		c.openedAt = c.Now()
	}
}

func isCircuitFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if IsKind(err, ErrUnavailable) {
		return true
	}
	var oe *Error
	if errors.As(err, &oe) {
		return false
	}
	return true
}

var _ JSONDoer = (*ProtectedClient)(nil)
