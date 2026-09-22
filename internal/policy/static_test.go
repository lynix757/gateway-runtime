package policy

import (
	"context"
	"reflect"
	"testing"
)

func TestStaticEvaluator(t *testing.T) {
	p := Static{
		RolePermissions: map[string][]string{
			"admin":  {"asset.write", "asset.read"},
			"viewer": {"asset.read"},
		},
		RoleCapabilities: map[string][]string{
			"admin": {"asset-admin"},
		},
	}
	subject := Subject{ID: "user-1", Roles: []string{"viewer", "admin"}}

	got, err := p.Evaluate(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Permissions, []string{"asset.read", "asset.write"}) {
		t.Fatalf("permissions = %#v", got.Permissions)
	}
	if !reflect.DeepEqual(got.Capabilities, []string{"asset-admin"}) {
		t.Fatalf("capabilities = %#v", got.Capabilities)
	}
	allowed, err := p.Allowed(context.Background(), subject, "asset.write")
	if err != nil || !allowed {
		t.Fatalf("allowed = %v, err = %v", allowed, err)
	}
}
