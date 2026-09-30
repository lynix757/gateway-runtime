package audit

import (
	"context"
	"log/slog"
	"os"
)

type SlogSink struct {
	Service  string
	Role     string
	Instance string
}

func (s SlogSink) Append(_ context.Context, e Event) error {
	actorType := "anonymous"
	if e.Actor != "" {
		actorType = "user"
	}
	service, role := "rebff", "rebff"
	if s.Service != "" {
		service = s.Service
	}
	if s.Role != "" {
		role = s.Role
	}
	instance := ""
	if s.Instance != "" {
		instance = s.Instance
	} else if hostname, err := os.Hostname(); err == nil {
		instance = hostname
	}
	metadata := make(map[string]string, len(e.Attributes))
	for key, value := range e.Attributes {
		if key != "reason" {
			metadata[key] = value
		}
	}
	reason := e.Attributes["reason"]
	outcome := map[string]any{"result": e.Outcome, "reason": reason}
	if e.HTTPStatus >= 100 && e.HTTPStatus <= 599 {
		outcome["http_status"] = e.HTTPStatus
	}

	slog.Info("audit",
		"schema_version", "1.0",
		"event_id", e.ID,
		"event_time", e.OccurredAt,
		"source", map[string]string{"service": service, "role": role, "instance": instance},
		"actor", map[string]string{"type": actorType, "subject": e.Actor, "username": e.ActorUsername},
		"client", map[string]string{"type": "browser", "ip": e.ClientIP, "ip_source": e.ClientIPSource},
		"action", e.Action,
		"target", map[string]string{"type": e.Target, "id": e.ResourceID},
		"outcome", outcome,
		"request", map[string]string{"request_id": e.CorrelationID, "trace_id": e.TraceID, "cf_ray": e.CFRay},
		"metadata", metadata,
	)
	return nil
}
