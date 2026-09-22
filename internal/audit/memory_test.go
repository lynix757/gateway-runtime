package audit

import (
	"context"
	"testing"
)

func TestMemorySink(t *testing.T) {
	sink := &MemorySink{}
	e := NewEvent("auth.login", "session", "success")
	e.Actor = "user-1"
	if err := sink.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	got := sink.Snapshot()
	if len(got) != 1 || got[0].Actor != "user-1" || got[0].ID == "" {
		t.Fatalf("unexpected events: %+v", got)
	}
}
