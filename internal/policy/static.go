package policy

import (
	"context"
	"sort"
)

type Static struct {
	RolePermissions  map[string][]string
	RoleCapabilities map[string][]string
}

func (p Static) Evaluate(_ context.Context, subject Subject) (Decision, error) {
	permSeen := map[string]struct{}{}
	capSeen := map[string]struct{}{}
	for _, role := range subject.Roles {
		for _, value := range p.RolePermissions[role] {
			permSeen[value] = struct{}{}
		}
		for _, value := range p.RoleCapabilities[role] {
			capSeen[value] = struct{}{}
		}
	}
	perms := keysSorted(permSeen)
	caps := keysSorted(capSeen)
	return Decision{
		Permissions:  perms,
		Capabilities: caps,
		AuthLevel:    "authenticated",
	}, nil
}

func (p Static) Allowed(ctx context.Context, subject Subject, permission string) (bool, error) {
	decision, err := p.Evaluate(ctx, subject)
	if err != nil {
		return false, err
	}
	for _, value := range decision.Permissions {
		if value == permission {
			return true, nil
		}
	}
	return false, nil
}

func keysSorted(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
