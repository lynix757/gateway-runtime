package policy

import "context"

type Subject struct {
	ID    string
	Roles []string
}

type Decision struct {
	Permissions  []string
	Capabilities []string
	AuthLevel    string
}

type Evaluator interface {
	Evaluate(ctx context.Context, subject Subject) (Decision, error)
	Allowed(ctx context.Context, subject Subject, permission string) (bool, error)
}
