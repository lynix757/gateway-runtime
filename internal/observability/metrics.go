package observability

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type httpKey struct{ Method, Route, StatusClass string }
type authKey struct{ Outcome string }
type decisionKey struct{ Outcome, Permission string }
type outboundKey struct{ Service, Method, Outcome string }
type rejectedKey struct{ Reason string }
type outboundRejectedKey struct{ Service, Reason string }

type histogram struct {
	Count   uint64
	Sum     float64
	Buckets []uint64
}

type Metrics struct {
	mu sync.RWMutex

	httpRequests map[httpKey]uint64
	httpDuration map[struct{ Method, Route string }]histogram
	authAttempts map[authKey]uint64
	decisions    map[decisionKey]uint64
	outRequests  map[outboundKey]uint64
	outDuration  map[struct{ Service, Method string }]histogram
	rejected     map[rejectedKey]uint64
	outRejected  map[outboundRejectedKey]uint64
	inflight     int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		httpRequests: make(map[httpKey]uint64),
		httpDuration: make(map[struct{ Method, Route string }]histogram),
		authAttempts: make(map[authKey]uint64),
		decisions:    make(map[decisionKey]uint64),
		outRequests:  make(map[outboundKey]uint64),
		outDuration:  make(map[struct{ Service, Method string }]histogram),
		rejected:     make(map[rejectedKey]uint64),
		outRejected:  make(map[outboundRejectedKey]uint64),
	}
}

func (m *Metrics) ensure() {
	if m.httpRequests == nil {
		m.httpRequests = make(map[httpKey]uint64)
		m.httpDuration = make(map[struct{ Method, Route string }]histogram)
		m.authAttempts = make(map[authKey]uint64)
		m.decisions = make(map[decisionKey]uint64)
		m.outRequests = make(map[outboundKey]uint64)
		m.outDuration = make(map[struct{ Service, Method string }]histogram)
		m.rejected = make(map[rejectedKey]uint64)
		m.outRejected = make(map[outboundRejectedKey]uint64)
	}
}

func (m *Metrics) RecordHTTPRequest(method, route string, status int, d time.Duration) {
	if m == nil {
		return
	}
	if route == "" {
		route = "unmatched"
	}
	method = strings.ToUpper(method)
	statusClass := fmt.Sprintf("%dxx", status/100)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.httpRequests[httpKey{method, route, statusClass}]++
	key := struct{ Method, Route string }{method, route}
	m.httpDuration[key] = observe(m.httpDuration[key], d.Seconds())
}

func (m *Metrics) RecordAuth(outcome string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.authAttempts[authKey{outcome}]++
}

func (m *Metrics) RecordAuthorization(outcome, permission string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.decisions[decisionKey{outcome, permission}]++
}

func (m *Metrics) RecordOutbound(service, method, outcome string, d time.Duration) {
	if m == nil {
		return
	}
	if service == "" {
		service = "unknown"
	}
	method = strings.ToUpper(method)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.outRequests[outboundKey{service, method, outcome}]++
	key := struct{ Service, Method string }{service, method}
	m.outDuration[key] = observe(m.outDuration[key], d.Seconds())
}

func (m *Metrics) RecordRejected(reason string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.rejected[rejectedKey{Reason: reason}]++
}

func (m *Metrics) RecordOutboundRejected(service, reason string) {
	if m == nil {
		return
	}
	if service == "" {
		service = "unknown"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.outRejected[outboundRejectedKey{Service: service, Reason: reason}]++
}

func (m *Metrics) AddInflight(delta int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inflight += delta
	if m.inflight < 0 {
		m.inflight = 0
	}
}

func observe(h histogram, value float64) histogram {
	if len(h.Buckets) == 0 {
		h.Buckets = make([]uint64, len(defaultBuckets))
	}
	h.Count++
	h.Sum += value
	for i, bound := range defaultBuckets {
		if value <= bound {
			h.Buckets[i]++
		}
	}
	return h
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.writePrometheus(w)
	})
}

func (m *Metrics) writePrometheus(w io.Writer) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fmt.Fprintln(w, "# TYPE rebff_http_requests_total counter")
	for _, k := range sortedHTTPKeys(m.httpRequests) {
		fmt.Fprintf(w, "rebff_http_requests_total{method=%q,route=%q,status_class=%q} %d\n",
			k.Method, k.Route, k.StatusClass, m.httpRequests[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_http_request_duration_seconds histogram")
	for _, k := range sortedHTTPDurationKeys(m.httpDuration) {
		writeHistogram(w, "rebff_http_request_duration_seconds",
			fmt.Sprintf("method=%q,route=%q", k.Method, k.Route), m.httpDuration[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_auth_attempts_total counter")
	for _, k := range sortedAuthKeys(m.authAttempts) {
		fmt.Fprintf(w, "rebff_auth_attempts_total{outcome=%q} %d\n", k.Outcome, m.authAttempts[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_authorization_decisions_total counter")
	for _, k := range sortedDecisionKeys(m.decisions) {
		fmt.Fprintf(w, "rebff_authorization_decisions_total{outcome=%q,permission=%q} %d\n",
			k.Outcome, k.Permission, m.decisions[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_outbound_requests_total counter")
	for _, k := range sortedOutboundKeys(m.outRequests) {
		fmt.Fprintf(w, "rebff_outbound_requests_total{service=%q,method=%q,outcome=%q} %d\n",
			k.Service, k.Method, k.Outcome, m.outRequests[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_http_rejected_total counter")
	for _, k := range sortedRejectedKeys(m.rejected) {
		fmt.Fprintf(w, "rebff_http_rejected_total{reason=%q} %d\n", k.Reason, m.rejected[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_outbound_rejected_total counter")
	for _, k := range sortedOutboundRejectedKeys(m.outRejected) {
		fmt.Fprintf(w, "rebff_outbound_rejected_total{service=%q,reason=%q} %d\n", k.Service, k.Reason, m.outRejected[k])
	}

	fmt.Fprintln(w, "# TYPE rebff_http_inflight_requests gauge")
	fmt.Fprintf(w, "rebff_http_inflight_requests %d\n", m.inflight)

	fmt.Fprintln(w, "# TYPE rebff_outbound_request_duration_seconds histogram")
	for _, k := range sortedOutboundDurationKeys(m.outDuration) {
		writeHistogram(w, "rebff_outbound_request_duration_seconds",
			fmt.Sprintf("service=%q,method=%q", k.Service, k.Method), m.outDuration[k])
	}
}

func writeHistogram(w io.Writer, name, labels string, h histogram) {
	for i, bound := range defaultBuckets {
		fmt.Fprintf(w, "%s_bucket{%s,le=%q} %d\n", name, labels, strconv.FormatFloat(bound, 'g', -1, 64), h.Buckets[i])
	}
	fmt.Fprintf(w, "%s_bucket{%s,le=%q} %d\n", name, labels, "+Inf", h.Count)
	fmt.Fprintf(w, "%s_sum{%s} %g\n", name, labels, h.Sum)
	fmt.Fprintf(w, "%s_count{%s} %d\n", name, labels, h.Count)
}

func sortedHTTPKeys(m map[httpKey]uint64) []httpKey {
	out := make([]httpKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Method+out[i].Route+out[i].StatusClass < out[j].Method+out[j].Route+out[j].StatusClass
	})
	return out
}
func sortedHTTPDurationKeys(m map[struct{ Method, Route string }]histogram) []struct{ Method, Route string } {
	out := make([]struct{ Method, Route string }, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Method+out[i].Route < out[j].Method+out[j].Route })
	return out
}
func sortedAuthKeys(m map[authKey]uint64) []authKey {
	out := make([]authKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Outcome < out[j].Outcome })
	return out
}
func sortedDecisionKeys(m map[decisionKey]uint64) []decisionKey {
	out := make([]decisionKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Outcome+out[i].Permission < out[j].Outcome+out[j].Permission })
	return out
}
func sortedOutboundKeys(m map[outboundKey]uint64) []outboundKey {
	out := make([]outboundKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Service+out[i].Method+out[i].Outcome < out[j].Service+out[j].Method+out[j].Outcome
	})
	return out
}
func sortedOutboundDurationKeys(m map[struct{ Service, Method string }]histogram) []struct{ Service, Method string } {
	out := make([]struct{ Service, Method string }, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service+out[i].Method < out[j].Service+out[j].Method })
	return out
}

func sortedRejectedKeys(m map[rejectedKey]uint64) []rejectedKey {
	out := make([]rejectedKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Reason < out[j].Reason })
	return out
}

func sortedOutboundRejectedKeys(m map[outboundRejectedKey]uint64) []outboundRejectedKey {
	out := make([]outboundRejectedKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Service+out[i].Reason < out[j].Service+out[j].Reason
	})
	return out
}
