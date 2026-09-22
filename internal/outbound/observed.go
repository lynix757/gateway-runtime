package outbound

import (
	"context"
	"time"

	"gateway-runtime/internal/observability"
)

type JSONDoer interface {
	DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error
}

type ObservedClient struct {
	Next        JSONDoer
	Metrics     *observability.Metrics
	ServiceName string
}

func (c ObservedClient) DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error {
	start := time.Now()
	err := c.Next.DoJSON(ctx, sessionID, method, path, in, out)
	if c.Metrics != nil {
		c.Metrics.RecordOutbound(c.ServiceName, method, outboundMetricOutcome(err), time.Since(start))
	}
	return err
}

func outboundMetricOutcome(err error) string {
	if err == nil {
		return "success"
	}
	switch {
	case IsKind(err, ErrRateLimited):
		return "rate_limited"
	case IsKind(err, ErrUnavailable):
		return "unavailable"
	case IsKind(err, ErrUnauthorized):
		return "unauthorized"
	case IsKind(err, ErrForbidden):
		return "forbidden"
	default:
		return "error"
	}
}

var _ JSONDoer = (*Client)(nil)
var _ JSONDoer = ObservedClient{}
