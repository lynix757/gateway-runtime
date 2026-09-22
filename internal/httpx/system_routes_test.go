package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadinessFailureDoesNotAffectLiveness(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSystemRoutes(mux, func(context.Context) error {
		return errors.New("redis unavailable")
	}, nil)

	readyReq := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	readyRec := httptest.NewRecorder()
	mux.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", readyRec.Code)
	}

	liveReq := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	liveRec := httptest.NewRecorder()
	mux.ServeHTTP(liveRec, liveReq)
	if liveRec.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200", liveRec.Code)
	}
}
