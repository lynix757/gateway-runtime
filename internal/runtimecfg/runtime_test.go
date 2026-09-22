package runtimecfg

import (
	"reflect"
	"testing"
)

func TestDefaultsToREBFF(t *testing.T) {
	roles, err := ParseRoles("")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roles, []Role{RoleREBFF}) {
		t.Fatalf("roles=%v", roles)
	}
	caps, err := ParseCapabilities(roles, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(caps[RoleREBFF]) == 0 {
		t.Fatal("missing default capabilities")
	}
}

func TestStorageShorthandAndDedup(t *testing.T) {
	roles, err := ParseRoles("storage")
	if err != nil {
		t.Fatal(err)
	}
	caps, err := ParseCapabilities(roles, "upload, download, upload")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caps[RoleStorage], []string{"upload", "download"}) {
		t.Fatalf("caps=%v", caps)
	}
}

func TestRejectCrossRoleCapability(t *testing.T) {
	if _, err := ParseCapabilities([]Role{RoleStorage}, "connect"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMultiRoleQualifiedCapabilities(t *testing.T) {
	roles, err := ParseRoles("storage,streaming")
	if err != nil {
		t.Fatal(err)
	}
	caps, err := ParseCapabilities(roles, "storage:upload|download;streaming:playback|token")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caps[RoleStorage], []string{"upload", "download"}) {
		t.Fatalf("storage=%v", caps[RoleStorage])
	}
	if !reflect.DeepEqual(caps[RoleStreaming], []string{"playback", "token"}) {
		t.Fatalf("streaming=%v", caps[RoleStreaming])
	}
}
