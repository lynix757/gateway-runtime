package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestSlogSinkWritesCompleteAuditRecord(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	e := NewEvent("order.cancel", "order", "success")
	e.Actor = "user-1"
	e.ActorUsername = "alice"
	e.ClientIP = "203.0.113.10"
	e.ClientIPSource = "cf-connecting-ip"
	e.CFRay = "8f1234567890abcd-BKK"
	e.ResourceID = "order-1"
	e.HTTPStatus = 200
	e.CorrelationID = "request-1"
	e.TraceID = "trace-1"
	if err := (SlogSink{Service: "rebff", Role: "rebff", Instance: "rebff-1"}).Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode audit log: %v: %s", err, buf.String())
	}
	if got["schema_version"] != "1.0" || got["event_time"] == "" || got["action"] != "order.cancel" {
		t.Fatalf("missing canonical envelope: %s", buf.String())
	}
	source := got["source"].(map[string]any)
	actor := got["actor"].(map[string]any)
	client := got["client"].(map[string]any)
	target := got["target"].(map[string]any)
	outcome := got["outcome"].(map[string]any)
	request := got["request"].(map[string]any)
	if source["service"] != "rebff" || source["role"] != "rebff" || source["instance"] != "rebff-1" {
		t.Fatalf("unexpected source: %s", buf.String())
	}
	if actor["subject"] != "user-1" || actor["username"] != "alice" || actor["type"] != "user" {
		t.Fatalf("unexpected actor: %s", buf.String())
	}
	if client["ip"] != "203.0.113.10" || client["ip_source"] != "cf-connecting-ip" {
		t.Fatalf("unexpected client: %s", buf.String())
	}
	if target["type"] != "order" || target["id"] != "order-1" {
		t.Fatalf("unexpected target: %s", buf.String())
	}
	if outcome["result"] != "success" || outcome["http_status"] != float64(200) {
		t.Fatalf("unexpected outcome: %s", buf.String())
	}
	if request["request_id"] != "request-1" || request["trace_id"] != "trace-1" || request["cf_ray"] != "8f1234567890abcd-BKK" {
		t.Fatalf("unexpected request: %s", buf.String())
	}
}

func TestSlogSinkOmitsUnknownHTTPStatus(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	e := NewEvent("authorization.allow", "asset.read", "allow")
	if err := (SlogSink{}).Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	outcome := got["outcome"].(map[string]any)
	if _, exists := outcome["http_status"]; exists {
		t.Fatalf("unexpected zero HTTP status: %s", buf.String())
	}
}
