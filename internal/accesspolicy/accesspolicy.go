package accesspolicy

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gateway-runtime/internal/outbound"
)

type Request struct {
	Subject string            `json:"subject"`
	Action  string            `json:"action"`
	Target  string            `json:"target"`
	Context map[string]string `json:"context,omitempty"`
}

type Decision struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
}

type Evaluator interface {
	Authorize(ctx context.Context, req Request) (Decision, error)
}

type Remote struct {
	HTTP *outbound.Client
}

func (r Remote) Authorize(ctx context.Context, req Request) (Decision, error) {
	if r.HTTP == nil {
		return Decision{}, fmt.Errorf("access policy client is not configured")
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.Action = strings.TrimSpace(req.Action)
	req.Target = strings.TrimSpace(req.Target)
	if req.Subject == "" || req.Action == "" || req.Target == "" {
		return Decision{}, fmt.Errorf("subject, action and target are required")
	}
	var out Decision
	if err := r.HTTP.DoJSON(ctx, "", http.MethodPost, "/v1/authorize", req, &out); err != nil {
		return Decision{}, err
	}
	return out, nil
}
