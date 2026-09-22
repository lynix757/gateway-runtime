package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"example.com/asset-portal/internal/asset"
	"example.com/asset-portal/internal/storage"
	"gateway-runtime/core"
)

var assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type uploadInput struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

func Register(r *core.Router) error {
	assetURL := strings.TrimSpace(os.Getenv("ASSET_API_URL"))
	if assetURL == "" {
		assetURL = "http://127.0.0.1:19001"
	}
	storageURL := strings.TrimSpace(os.Getenv("STORAGE_API_URL"))
	if storageURL == "" {
		storageURL = "http://127.0.0.1:19002"
	}

	assetHTTP, err := r.NewJSONClient("asset-api", assetURL, core.ClientOptions{
		Timeout:          3 * time.Second,
		MaxConcurrent:    64,
		FailureThreshold: 5,
		OpenFor:          15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("asset client: %w", err)
	}
	storageHTTP, err := r.NewJSONClient("storage-api", storageURL, core.ClientOptions{
		Timeout:          3 * time.Second,
		MaxConcurrent:    32,
		FailureThreshold: 5,
		OpenFor:          15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("storage client: %w", err)
	}

	assets := asset.Client{HTTP: assetHTTP}
	storageClient := storage.Client{HTTP: storageHTTP}

	r.HandleFunc("GET /api/assets/{id}", "asset.read", func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		if !assetIDPattern.MatchString(id) {
			http.Error(w, "invalid asset id", http.StatusBadRequest)
			return
		}
		sessionID, ok := core.SessionID(req)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		result, err := assets.Get(req.Context(), sessionID, id)
		if err != nil {
			core.WriteError(w, err)
			return
		}
		core.WriteJSON(w, http.StatusOK, result)
	})

	r.HandleFunc("POST /api/assets/{id}/attachments/upload-url", "storage.upload", func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		if !assetIDPattern.MatchString(id) {
			http.Error(w, "invalid asset id", http.StatusBadRequest)
			return
		}
		sessionID, ok := core.SessionID(req)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var in uploadInput
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if !allowedContentType(in.ContentType) {
			http.Error(w, "unsupported content type", http.StatusBadRequest)
			return
		}
		filename := path.Base(strings.TrimSpace(in.Filename))
		if filename == "." || filename == "/" || filename == "" || len(filename) > 128 {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}

		objectKey := fmt.Sprintf("assets/%s/attachments/%d-%s", id, time.Now().UnixNano(), filename)
		result, err := storageClient.PresignUpload(req.Context(), sessionID, storage.UploadRequest{
			ObjectKey:   objectKey,
			ContentType: in.ContentType,
			TTLSeconds:  300,
		})
		if err != nil {
			core.WriteError(w, err)
			return
		}
		core.WriteJSON(w, http.StatusOK, result)
	})

	return nil
}

func allowedContentType(v string) bool {
	switch v {
	case "application/pdf", "image/jpeg", "image/png":
		return true
	default:
		return false
	}
}
