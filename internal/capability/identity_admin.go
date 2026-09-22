package capability

import "context"

type IdentityAdmin interface {
	DisableUser(ctx context.Context, subject string) error
	EnableUser(ctx context.Context, subject string) error
	SendRequiredAction(ctx context.Context, subject string, actions []string) error
}
