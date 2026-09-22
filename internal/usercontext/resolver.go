package usercontext

import (
	"context"

	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
)

type SessionResolver struct {
	Sessions session.Store
	Policy   policy.Evaluator
}

func (r SessionResolver) Resolve(ctx context.Context, sessionID string) (Context, error) {
	s, err := r.Sessions.Get(ctx, sessionID)
	if err != nil {
		return Context{}, err
	}

	var permissions, capabilities []string
	authLevel := "authenticated"
	if r.Policy != nil {
		decision, err := r.Policy.Evaluate(ctx, policy.Subject{
			ID:    s.Subject,
			Roles: append([]string(nil), s.Roles...),
		})
		if err != nil {
			return Context{}, err
		}
		permissions = decision.Permissions
		capabilities = decision.Capabilities
		if decision.AuthLevel != "" {
			authLevel = decision.AuthLevel
		}
	}

	return Context{
		Subject:      s.Subject,
		DisplayName:  s.DisplayName,
		Email:        s.Email,
		Roles:        append([]string(nil), s.Roles...),
		Permissions:  permissions,
		AuthLevel:    authLevel,
		Capabilities: capabilities,
	}, nil
}
