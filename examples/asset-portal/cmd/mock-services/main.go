package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type asset struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Serial     string `json:"serial"`
	Status     string `json:"status"`
	ProjectID  string `json:"project_id"`
	ProvinceID string `json:"province_id"`
	Province   string `json:"province"`
}

type principal struct {
	Subject          string
	Username         string
	AllowedProvinces map[string]bool
}

type mockService string

const (
	mockServiceAll     mockService = "all"
	mockServiceAsset   mockService = "asset"
	mockServiceStorage mockService = "storage"
)

var mockAssets = map[string]asset{
	"A001": {ID: "A001", Name: "Demo Notebook", Serial: "SN-DEMO-001", Status: "active", ProjectID: "PRJ-001", ProvinceID: "50", Province: "Chiang Mai"},
	"A002": {ID: "A002", Name: "Bangkok Demo Server", Serial: "SN-DEMO-002", Status: "active", ProjectID: "PRJ-002", ProvinceID: "10", Province: "Bangkok"},
}

func main() {
	mode, err := parseMockService(os.Getenv("MOCK_SERVICE"))
	if err != nil {
		log.Fatal(err)
	}
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	switch mode {
	case mockServiceAsset:
		logger.Println("component=asset-api event=start addr=:19001")
		log.Fatal(http.ListenAndServe(":19001", newAssetHandler(logger)))
	case mockServiceStorage:
		logger.Println("component=storage-api event=start addr=:19002")
		log.Fatal(http.ListenAndServe(":19002", newStorageHandler(logger)))
	default:
		go func() {
			logger.Println("component=asset-api event=start addr=:19001")
			log.Fatal(http.ListenAndServe(":19001", newAssetHandler(logger)))
		}()
		logger.Println("component=storage-api event=start addr=:19002")
		log.Fatal(http.ListenAndServe(":19002", newStorageHandler(logger)))
	}
}

func parseMockService(value string) (mockService, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "all":
		return mockServiceAll, nil
	case "asset":
		return mockServiceAsset, nil
	case "storage":
		return mockServiceStorage, nil
	default:
		return "", errors.New("MOCK_SERVICE must be one of: asset, storage, all")
	}
}

func newAssetHandler(loggers ...*log.Logger) http.Handler {
	logger := log.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		traceID := r.Header.Get("X-Trace-ID")
		assetID := r.PathValue("id")
		logger.Printf("component=asset-api event=request method=%s path=%s request_id=%s trace_id=%s asset_id=%s", r.Method, r.URL.Path, requestID, traceID, assetID)

		p, ok := resolveMockPrincipal(r.Header.Get("Authorization"))
		if !ok {
			logger.Printf("component=asset-api event=identity request_id=%s trace_id=%s asset_id=%s decision=reject reason=invalid_bearer", requestID, traceID, assetID)
			http.Error(w, "invalid bearer identity", http.StatusUnauthorized)
			return
		}
		logger.Printf("component=asset-api event=identity request_id=%s trace_id=%s subject=%s username=%s allowed_provinces=%s", requestID, traceID, p.Subject, p.Username, strings.Join(provinceKeys(p.AllowedProvinces), ","))

		item, ok := mockAssets[assetID]
		if !ok {
			logger.Printf("component=asset-api event=resource request_id=%s trace_id=%s username=%s asset_id=%s decision=not_found", requestID, traceID, p.Username, assetID)
			http.NotFound(w, r)
			return
		}

		if !p.AllowedProvinces[item.ProvinceID] {
			logger.Printf("component=asset-api event=authorization request_id=%s trace_id=%s username=%s asset_id=%s asset_province=%s allowed_provinces=%s decision=deny", requestID, traceID, p.Username, item.ID, item.ProvinceID, strings.Join(provinceKeys(p.AllowedProvinces), ","))
			writeJSONStatus(w, http.StatusForbidden, map[string]any{
				"error": "forbidden", "reason": "province_scope_denied", "subject": p.Subject,
				"username": p.Username, "asset_id": item.ID, "asset_province_id": item.ProvinceID,
				"allowed_provinces": provinceKeys(p.AllowedProvinces),
			})
			return
		}

		logger.Printf("component=asset-api event=authorization request_id=%s trace_id=%s username=%s asset_id=%s asset_province=%s allowed_provinces=%s decision=allow", requestID, traceID, p.Username, item.ID, item.ProvinceID, strings.Join(provinceKeys(p.AllowedProvinces), ","))
		writeJSON(w, item)
	})
	return withResponseLog("asset-api", logger, mux)
}

func newStorageHandler(loggers ...*log.Logger) http.Handler {
	logger := log.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/presign/upload", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		traceID := r.Header.Get("X-Trace-ID")
		logger.Printf("component=storage-api event=request method=%s path=%s request_id=%s trace_id=%s", r.Method, r.URL.Path, requestID, traceID)
		var in struct {
			ObjectKey string `json:"object_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			logger.Printf("component=storage-api event=presign request_id=%s trace_id=%s decision=reject reason=bad_request", requestID, traceID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		logger.Printf("component=storage-api event=presign request_id=%s trace_id=%s object_key=%s decision=allow", requestID, traceID, in.ObjectKey)
		writeJSON(w, map[string]any{
			"method":     "PUT",
			"url":        "https://storage.example.invalid/upload/" + in.ObjectKey,
			"expires_at": time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		})
	}))
	return withResponseLog("storage-api", logger, mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func withResponseLog(component string, logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Printf(
			"component=%s event=response method=%s path=%s request_id=%s trace_id=%s status=%d duration_ms=%d",
			component, r.Method, r.URL.Path, r.Header.Get("X-Request-ID"), r.Header.Get("X-Trace-ID"), status, time.Since(started).Milliseconds(),
		)
	})
}

// resolveMockPrincipal exists only for the demo service.
// Production services must cryptographically validate JWT signature, issuer,
// audience and expiry before constructing a Principal.
func resolveMockPrincipal(authz string) (principal, bool) {
	if !strings.HasPrefix(authz, "Bearer ") {
		return principal{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if token == "" {
		return principal{}, false
	}

	if token == "alice-token" {
		return alicePrincipal("demo-alice"), true
	}
	if token == "bob-token" {
		return principal{Subject: "demo-bob", Username: "bob", AllowedProvinces: map[string]bool{"10": true}}, true
	}

	claims, ok := decodeJWTPayloadForMock(token)
	if !ok {
		return principal{}, false
	}
	username, _ := claims["preferred_username"].(string)
	email, _ := claims["email"].(string)
	subject, _ := claims["sub"].(string)

	switch {
	case username == "alice", email == "alice@example.com":
		return alicePrincipal(subject), true
	case username == "bob", email == "bob@example.com":
		return principal{Subject: subject, Username: "bob", AllowedProvinces: map[string]bool{"10": true}}, true
	default:
		return principal{}, false
	}
}

func alicePrincipal(subject string) principal {
	return principal{Subject: subject, Username: "alice", AllowedProvinces: map[string]bool{"50": true}}
}

func decodeJWTPayloadForMock(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, false
	}
	return claims, true
}

func provinceKeys(in map[string]bool) []string {
	out := make([]string, 0, len(in))
	for id, allowed := range in {
		if allowed {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func requireBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing Bearer token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
