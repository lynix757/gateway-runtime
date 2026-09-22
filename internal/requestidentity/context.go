package requestidentity

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	DefaultActorHeader   = "X-Actor-Subject"
	DefaultServiceHeader = "X-Service-Identity"
	LegacySubjectHeader  = "X-Auth-Subject"
)

type Identity struct {
	Actor     string `json:"actor"`
	Service   string `json:"service,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}

type Headers struct {
	Actor   string
	Service string
	Legacy  string
}

func Extract(r *http.Request, headers Headers) Identity {
	actorHeader := strings.TrimSpace(headers.Actor)
	if actorHeader == "" {
		actorHeader = DefaultActorHeader
	}
	serviceHeader := strings.TrimSpace(headers.Service)
	if serviceHeader == "" {
		serviceHeader = DefaultServiceHeader
	}
	legacyHeader := strings.TrimSpace(headers.Legacy)
	if legacyHeader == "" {
		legacyHeader = LegacySubjectHeader
	}

	actor := strings.TrimSpace(r.Header.Get(actorHeader))
	if actor == "" {
		actor = strings.TrimSpace(r.Header.Get(legacyHeader))
	}

	traceID := strings.TrimSpace(r.Header.Get("X-Trace-ID"))
	if traceID == "" {
		traceID = strings.TrimSpace(r.Header.Get("traceparent"))
	}

	return Identity{
		Actor:     actor,
		Service:   strings.TrimSpace(r.Header.Get(serviceHeader)),
		TenantID:  strings.TrimSpace(r.Header.Get("X-Tenant-ID")),
		ProjectID: strings.TrimSpace(r.Header.Get("X-Project-ID")),
		RequestID: strings.TrimSpace(r.Header.Get("X-Request-ID")),
		TraceID:   traceID,
	}
}

func (i Identity) ValidateActor() error {
	if strings.TrimSpace(i.Actor) == "" {
		return fmt.Errorf("actor required")
	}
	return nil
}

func (i Identity) PolicyContext() map[string]string {
	out := map[string]string{}
	if i.Service != "" {
		out["service_id"] = i.Service
	}
	if i.TenantID != "" {
		out["tenant_id"] = i.TenantID
	}
	if i.ProjectID != "" {
		out["project_id"] = i.ProjectID
	}
	if i.RequestID != "" {
		out["request_id"] = i.RequestID
	}
	if i.TraceID != "" {
		out["trace_id"] = i.TraceID
	}
	return out
}
