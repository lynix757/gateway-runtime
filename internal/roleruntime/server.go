package roleruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gateway-runtime/internal/accesspolicy"
	"gateway-runtime/internal/capability"
	"gateway-runtime/internal/requestidentity"
	"gateway-runtime/internal/runtimecfg"
	"gateway-runtime/internal/storagecontract"
	"gateway-runtime/internal/storageprovider"
	"gateway-runtime/internal/storagetarget"
	"gateway-runtime/internal/workloadauth"
)

type Config struct {
	Addr                 string
	BasePath             string
	Selection            runtimecfg.Selection
	StorageSigner        capability.StorageSigner
	StorageProviders     storageprovider.Registry
	StorageDefaultTTL    time.Duration
	StorageMaxTTL        time.Duration
	StorageTargets       storagetarget.Registry
	StorageAccessPolicy  accesspolicy.Evaluator
	StorageSubjectHeader string
	WorkloadVerifier     workloadauth.Verifier
	WorkloadAuthRequired bool
}

type storageRequest struct {
	Target           string `json:"target,omitempty"`
	Operation        string `json:"operation,omitempty"`
	Bucket           string `json:"bucket"`
	ObjectKey        string `json:"object_key"`
	ContentType      string `json:"content_type,omitempty"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
}

type verificationRequest struct {
	Target              string            `json:"target"`
	ObjectKey           string            `json:"object_key"`
	ExpectedSize        *int64            `json:"expected_size,omitempty"`
	ExpectedContentType string            `json:"expected_content_type,omitempty"`
	ExpectedChecksums   map[string]string `json:"expected_checksums,omitempty"`
}

type multipartRequest struct {
	Target           string                     `json:"target,omitempty"`
	Bucket           string                     `json:"bucket"`
	ObjectKey        string                     `json:"object_key"`
	ContentType      string                     `json:"content_type,omitempty"`
	UploadID         string                     `json:"upload_id,omitempty"`
	PartNumber       int                        `json:"part_number,omitempty"`
	ExpiresInSeconds int64                      `json:"expires_in_seconds,omitempty"`
	Parts            []capability.CompletedPart `json:"parts,omitempty"`
}

func Run(cfg Config) error {
	mux := http.NewServeMux()
	registerSystem(mux, cfg)
	for _, role := range cfg.Selection.Roles {
		registerRole(mux, cfg, role, cfg.Selection.Capabilities[role])
	}

	var handler http.Handler = mux
	if cfg.BasePath != "" && cfg.BasePath != "/" {
		base := strings.TrimRight(cfg.BasePath, "/")
		root := http.NewServeMux()
		root.Handle(base+"/", http.StripPrefix(base, mux))
		handler = root
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("gateway runtime listening",
			"addr", cfg.Addr,
			"roles", cfg.Selection.RoleNames(),
			"capabilities", cfg.Selection.Capabilities,
			"storage_signer_configured", cfg.StorageSigner != nil,
		)
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func registerSystem(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "roles": cfg.Selection.RoleNames()})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		if cfg.Selection.HasRole(runtimecfg.RoleStorage) && cfg.WorkloadAuthRequired && cfg.WorkloadVerifier == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "workload_auth_not_configured"})
			return
		}
		if cfg.Selection.HasRole(runtimecfg.RoleStorage) && len(cfg.StorageTargets) > 0 && cfg.StorageAccessPolicy == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "storage_access_policy_not_configured"})
			return
		}
		if cfg.Selection.HasRole(runtimecfg.RoleStorage) && storageProviderRequired(cfg.Selection) {
			if err := validateStorageProvidersReady(cfg); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "roles": cfg.Selection.RoleNames()})
	})
	mux.HandleFunc("GET /runtime", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"roles":        cfg.Selection.RoleNames(),
			"capabilities": cfg.Selection.Capabilities,
			"providers": map[string]any{
				"storage_signer":         cfg.StorageSigner != nil,
				"storage_provider_count": len(cfg.StorageProviders),
				"storage_access_policy":  cfg.StorageAccessPolicy != nil,
			},
		})
	})
}

func storageSignerForTarget(cfg Config, targetName string) (capability.StorageSigner, bool) {
	if len(cfg.StorageProviders) == 0 {
		return cfg.StorageSigner, cfg.StorageSigner != nil
	}
	providerName := "default"
	if len(cfg.StorageTargets) > 0 {
		target, ok := cfg.StorageTargets.Resolve(strings.TrimSpace(targetName))
		if !ok {
			return nil, false
		}
		providerName = target.Provider
	}
	provider, ok := cfg.StorageProviders.Resolve(providerName)
	if !ok || provider.Signer == nil {
		return nil, false
	}
	return provider.Signer, true
}

func validateStorageProvidersReady(cfg Config) error {
	if len(cfg.StorageProviders) == 0 {
		if cfg.StorageSigner == nil {
			return fmt.Errorf("storage_signer_not_configured")
		}
		if hasCapability(cfg.Selection, runtimecfg.RoleStorage, "multipart") {
			if _, ok := cfg.StorageSigner.(capability.MultipartStorageSigner); !ok {
				return fmt.Errorf("storage_multipart_not_supported")
			}
		}
		if hasCapability(cfg.Selection, runtimecfg.RoleStorage, "metadata") {
			if _, ok := cfg.StorageSigner.(capability.StorageMetadataProvider); !ok {
				return fmt.Errorf("storage_metadata_not_supported")
			}
		}
		return nil
	}

	providerNames := map[string]struct{}{}
	if len(cfg.StorageTargets) == 0 {
		providerNames["default"] = struct{}{}
	} else {
		for _, target := range cfg.StorageTargets {
			providerNames[target.Provider] = struct{}{}
		}
	}
	for name := range providerNames {
		provider, ok := cfg.StorageProviders.Resolve(name)
		if !ok || provider.Signer == nil {
			return fmt.Errorf("storage_provider_not_configured")
		}
		if hasCapability(cfg.Selection, runtimecfg.RoleStorage, "multipart") {
			if _, ok := provider.Signer.(capability.MultipartStorageSigner); !ok {
				return fmt.Errorf("storage_multipart_not_supported")
			}
		}
		if hasCapability(cfg.Selection, runtimecfg.RoleStorage, "metadata") {
			if _, ok := provider.Signer.(capability.StorageMetadataProvider); !ok {
				return fmt.Errorf("storage_metadata_not_supported")
			}
		}
	}
	return nil
}

func storageProviderRequired(selection runtimecfg.Selection) bool {
	for _, c := range selection.Capabilities[runtimecfg.RoleStorage] {
		switch c {
		case "upload", "download", "presign", "multipart", "metadata":
			return true
		}
	}
	return false
}

func registerRole(mux *http.ServeMux, cfg Config, role runtimecfg.Role, caps []string) {
	for _, c := range caps {
		switch role {
		case runtimecfg.RoleStorage:
			registerStorageCapability(mux, cfg, c)
		default:
			registerUnavailableCapability(mux, role, c)
		}
	}
}

func registerStorageCapability(mux *http.ServeMux, cfg Config, c string) {
	switch c {
	case "upload":
		mux.HandleFunc("POST /storage/upload", func(w http.ResponseWriter, r *http.Request) {
			req, target, ok := decodeStorageRequest(w, r, cfg)
			if !ok {
				return
			}
			if !authorizeStorage(w, r, cfg, target, "storage.upload", true) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			op, err := signer.PresignPut(r.Context(), req)
			writeStorageResult(w, op, err)
		})
		mux.HandleFunc("POST /storage/intents/upload", func(w http.ResponseWriter, r *http.Request) {
			req, target, ok := decodeStorageRequest(w, r, cfg)
			if !ok {
				return
			}
			intent := storagecontract.UploadIntent{
				Target: target, ObjectKey: req.ObjectKey, ContentType: req.ContentType, ExpiresIn: req.ExpiresIn,
			}
			if err := intent.Validate(); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if !authorizeStorage(w, r, cfg, target, "storage.upload", true) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			op, err := signer.PresignPut(r.Context(), req)
			if err != nil {
				writeStorageProviderError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, storagecontract.UploadGrantFromOperation(op))
		})
	case "download":
		mux.HandleFunc("GET /storage/download", func(w http.ResponseWriter, r *http.Request) {
			req, target, ok := storageRequestFromQuery(w, r, cfg)
			if !ok {
				return
			}
			if !authorizeStorage(w, r, cfg, target, "storage.download", false) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			op, err := signer.PresignGet(r.Context(), req)
			writeStorageResult(w, op, err)
		})
		mux.HandleFunc("POST /storage/intents/download", func(w http.ResponseWriter, r *http.Request) {
			req, target, ok := decodeStorageRequest(w, r, cfg)
			if !ok {
				return
			}
			intent := storagecontract.DownloadIntent{
				Target: target, ObjectKey: req.ObjectKey, ExpiresIn: req.ExpiresIn,
			}
			if err := intent.Validate(); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if !authorizeStorage(w, r, cfg, target, "storage.download", false) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			op, err := signer.PresignGet(r.Context(), req)
			if err != nil {
				writeStorageProviderError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, storagecontract.DownloadGrantFromOperation(op))
		})
	case "presign":
		mux.HandleFunc("POST /storage/presign", func(w http.ResponseWriter, r *http.Request) {
			var in storageRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			req, err := validateStorageRequest(in, cfg)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			var op capability.PresignedOperation
			switch strings.ToLower(strings.TrimSpace(in.Operation)) {
			case "put", "upload":
				if !authorizeStorage(w, r, cfg, in.Target, "storage.upload", true) {
					return
				}
				signer, ok := storageSignerForTarget(cfg, in.Target)
				if !ok {
					providerUnavailable(w, runtimecfg.RoleStorage, c)
					return
				}
				op, err = signer.PresignPut(r.Context(), req)
			case "get", "download":
				if !authorizeStorage(w, r, cfg, in.Target, "storage.download", false) {
					return
				}
				signer, ok := storageSignerForTarget(cfg, in.Target)
				if !ok {
					providerUnavailable(w, runtimecfg.RoleStorage, c)
					return
				}
				op, err = signer.PresignGet(r.Context(), req)
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "operation must be put/upload or get/download"})
				return
			}
			writeStorageResult(w, op, err)
		})
	case "multipart":
		registerMultipartRoutes(mux, cfg)
	case "metadata":
		mux.HandleFunc("GET /storage/metadata", func(w http.ResponseWriter, r *http.Request) {
			base, target, ok := storageRequestFromQuery(w, r, cfg)
			if !ok {
				return
			}
			if !authorizeStorage(w, r, cfg, target, "storage.metadata", false) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			provider, supported := signer.(capability.StorageMetadataProvider)
			if !supported {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			out, err := provider.GetMetadata(r.Context(), capability.StorageMetadataRequest{
				Bucket: base.Bucket, ObjectKey: base.ObjectKey,
			})
			if err != nil {
				writeStorageProviderError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, out)
		})
		mux.HandleFunc("POST /storage/intents/verify", func(w http.ResponseWriter, r *http.Request) {
			var in verificationRequest
			if !decodeJSONBody(w, r, &in) {
				return
			}
			intent := storagecontract.UploadVerificationIntent{
				Target:              strings.TrimSpace(in.Target),
				ObjectKey:           strings.TrimSpace(in.ObjectKey),
				ExpectedSize:        in.ExpectedSize,
				ExpectedContentType: strings.TrimSpace(in.ExpectedContentType),
				ExpectedChecksums:   in.ExpectedChecksums,
			}
			if err := intent.Validate(); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			base, err := validateStorageRequest(storageRequest{
				Target: intent.Target, ObjectKey: intent.ObjectKey,
			}, cfg)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if !authorizeStorage(w, r, cfg, intent.Target, "storage.metadata", false) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, intent.Target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			provider, supported := signer.(capability.StorageMetadataProvider)
			if !supported {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			metadata, err := provider.GetMetadata(r.Context(), capability.StorageMetadataRequest{
				Bucket: base.Bucket, ObjectKey: base.ObjectKey,
			})
			if err != nil {
				writeStorageProviderError(w, err)
				return
			}
			result, err := storagecontract.VerifyUpload(intent, metadata)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, result)
		})
		mux.HandleFunc("GET /storage/references/metadata", func(w http.ResponseWriter, r *http.Request) {
			base, target, ok := storageRequestFromQuery(w, r, cfg)
			if !ok {
				return
			}
			if !authorizeStorage(w, r, cfg, target, "storage.metadata", false) {
				return
			}
			signer, ok := storageSignerForTarget(cfg, target)
			if !ok {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			provider, supported := signer.(capability.StorageMetadataProvider)
			if !supported {
				providerUnavailable(w, runtimecfg.RoleStorage, c)
				return
			}
			out, err := provider.GetMetadata(r.Context(), capability.StorageMetadataRequest{
				Bucket: base.Bucket, ObjectKey: base.ObjectKey,
			})
			if err != nil {
				writeStorageProviderError(w, err)
				return
			}
			ref := storagecontract.ReferenceFromMetadata(target, out)
			if err := ref.Validate(); err != nil {
				writeStorageProviderError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, ref)
		})
	}
}

func registerMultipartRoutes(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc("POST /storage/multipart/initiate", func(w http.ResponseWriter, r *http.Request) {
		var in multipartRequest
		if !decodeJSONBody(w, r, &in) {
			return
		}
		base, err := validateStorageRequest(storageRequest{
			Target: in.Target, Bucket: in.Bucket, ObjectKey: in.ObjectKey, ContentType: in.ContentType, ExpiresInSeconds: in.ExpiresInSeconds,
		}, cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if !authorizeStorage(w, r, cfg, in.Target, "storage.multipart", true) {
			return
		}
		baseSigner, found := storageSignerForTarget(cfg, in.Target)
		if !found {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		signer, ok := baseSigner.(capability.MultipartStorageSigner)
		if !ok {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		out, err := signer.InitiateMultipart(r.Context(), capability.MultipartInitiateRequest{
			Bucket: base.Bucket, ObjectKey: base.ObjectKey, ContentType: base.ContentType, ExpiresIn: base.ExpiresIn,
		})
		if err != nil {
			writeStorageProviderError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /storage/multipart/part", func(w http.ResponseWriter, r *http.Request) {
		var in multipartRequest
		if !decodeJSONBody(w, r, &in) {
			return
		}
		base, err := validateStorageRequest(storageRequest{
			Target: in.Target, Bucket: in.Bucket, ObjectKey: in.ObjectKey, ExpiresInSeconds: in.ExpiresInSeconds,
		}, cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		uploadID := strings.TrimSpace(in.UploadID)
		if uploadID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload_id is required"})
			return
		}
		if in.PartNumber < 1 || in.PartNumber > 10000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "part_number must be between 1 and 10000"})
			return
		}
		if !authorizeStorage(w, r, cfg, in.Target, "storage.multipart", true) {
			return
		}
		baseSigner, found := storageSignerForTarget(cfg, in.Target)
		if !found {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		signer, ok := baseSigner.(capability.MultipartStorageSigner)
		if !ok {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		op, err := signer.PresignMultipartPart(r.Context(), capability.MultipartPartRequest{
			Bucket: base.Bucket, ObjectKey: base.ObjectKey, UploadID: uploadID, PartNumber: in.PartNumber, ExpiresIn: base.ExpiresIn,
		})
		writeStorageResult(w, op, err)
	})

	mux.HandleFunc("POST /storage/multipart/complete", func(w http.ResponseWriter, r *http.Request) {
		var in multipartRequest
		if !decodeJSONBody(w, r, &in) {
			return
		}
		base, err := validateStorageRequest(storageRequest{Target: in.Target, Bucket: in.Bucket, ObjectKey: in.ObjectKey}, cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		uploadID := strings.TrimSpace(in.UploadID)
		if uploadID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload_id is required"})
			return
		}
		parts, err := validateCompletedParts(in.Parts)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if !authorizeStorage(w, r, cfg, in.Target, "storage.multipart", true) {
			return
		}
		baseSigner, found := storageSignerForTarget(cfg, in.Target)
		if !found {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		signer, ok := baseSigner.(capability.MultipartStorageSigner)
		if !ok {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		out, err := signer.CompleteMultipart(r.Context(), capability.MultipartCompleteRequest{
			Bucket: base.Bucket, ObjectKey: base.ObjectKey, UploadID: uploadID, Parts: parts,
		})
		if err != nil {
			writeStorageProviderError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /storage/multipart/abort", func(w http.ResponseWriter, r *http.Request) {
		var in multipartRequest
		if !decodeJSONBody(w, r, &in) {
			return
		}
		base, err := validateStorageRequest(storageRequest{Target: in.Target, Bucket: in.Bucket, ObjectKey: in.ObjectKey}, cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		uploadID := strings.TrimSpace(in.UploadID)
		if uploadID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload_id is required"})
			return
		}
		if !authorizeStorage(w, r, cfg, in.Target, "storage.multipart", true) {
			return
		}
		baseSigner, found := storageSignerForTarget(cfg, in.Target)
		if !found {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		signer, ok := baseSigner.(capability.MultipartStorageSigner)
		if !ok {
			providerUnavailable(w, runtimecfg.RoleStorage, "multipart")
			return
		}
		if err := signer.AbortMultipart(r.Context(), capability.MultipartAbortRequest{
			Bucket: base.Bucket, ObjectKey: base.ObjectKey, UploadID: uploadID,
		}); err != nil {
			writeStorageProviderError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func validateCompletedParts(parts []capability.CompletedPart) ([]capability.CompletedPart, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("parts are required")
	}
	if len(parts) > 10000 {
		return nil, fmt.Errorf("too many parts")
	}
	seen := make(map[int]struct{}, len(parts))
	out := make([]capability.CompletedPart, 0, len(parts))
	for _, part := range parts {
		if part.PartNumber < 1 || part.PartNumber > 10000 {
			return nil, fmt.Errorf("part_number must be between 1 and 10000")
		}
		if strings.TrimSpace(part.ETag) == "" {
			return nil, fmt.Errorf("etag is required for every part")
		}
		if _, exists := seen[part.PartNumber]; exists {
			return nil, fmt.Errorf("duplicate part_number")
		}
		seen[part.PartNumber] = struct{}{}
		out = append(out, capability.CompletedPart{PartNumber: part.PartNumber, ETag: strings.TrimSpace(part.ETag)})
	}
	return out, nil
}

func hasCapability(selection runtimecfg.Selection, role runtimecfg.Role, want string) bool {
	for _, c := range selection.Capabilities[role] {
		if c == want {
			return true
		}
	}
	return false
}

func decodeStorageRequest(w http.ResponseWriter, r *http.Request, cfg Config) (capability.PresignedRequest, string, bool) {
	var in storageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return capability.PresignedRequest{}, "", false
	}
	req, err := validateStorageRequest(in, cfg)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return capability.PresignedRequest{}, "", false
	}
	return req, strings.TrimSpace(in.Target), true
}

func storageRequestFromQuery(w http.ResponseWriter, r *http.Request, cfg Config) (capability.PresignedRequest, string, bool) {
	seconds := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("expires_in_seconds")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expires_in_seconds"})
			return capability.PresignedRequest{}, "", false
		}
		seconds = v
	}
	req, err := validateStorageRequest(storageRequest{
		Target:           r.URL.Query().Get("target"),
		Bucket:           r.URL.Query().Get("bucket"),
		ObjectKey:        r.URL.Query().Get("object_key"),
		ExpiresInSeconds: seconds,
	}, cfg)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return capability.PresignedRequest{}, "", false
	}
	return req, strings.TrimSpace(r.URL.Query().Get("target")), true
}

func validateStorageRequest(in storageRequest, cfg Config) (capability.PresignedRequest, error) {
	targetName := strings.TrimSpace(in.Target)
	bucket := strings.TrimSpace(in.Bucket)
	if len(cfg.StorageTargets) > 0 {
		if targetName == "" {
			return capability.PresignedRequest{}, fmt.Errorf("target is required")
		}
		target, ok := cfg.StorageTargets.Resolve(targetName)
		if !ok {
			return capability.PresignedRequest{}, fmt.Errorf("unknown storage target")
		}
		bucket = target.Bucket
	} else if bucket == "" {
		return capability.PresignedRequest{}, fmt.Errorf("bucket is required")
	}
	key := strings.TrimSpace(in.ObjectKey)
	if key == "" {
		return capability.PresignedRequest{}, fmt.Errorf("object_key is required")
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, "..") || strings.Contains(key, "\\") {
		return capability.PresignedRequest{}, fmt.Errorf("object_key is invalid")
	}
	ttl := cfg.StorageDefaultTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if in.ExpiresInSeconds > 0 {
		ttl = time.Duration(in.ExpiresInSeconds) * time.Second
	}
	maxTTL := cfg.StorageMaxTTL
	if maxTTL <= 0 {
		maxTTL = time.Hour
	}
	if ttl <= 0 || ttl > maxTTL {
		return capability.PresignedRequest{}, fmt.Errorf("expires_in_seconds exceeds allowed TTL")
	}
	return capability.PresignedRequest{
		Bucket:      bucket,
		ObjectKey:   key,
		ContentType: strings.TrimSpace(in.ContentType),
		ExpiresIn:   ttl,
	}, nil
}

func authorizeStorage(w http.ResponseWriter, r *http.Request, cfg Config, targetName, action string, write bool) bool {
	if len(cfg.StorageTargets) == 0 {
		return true
	}
	targetName = strings.TrimSpace(targetName)
	target, ok := cfg.StorageTargets.Resolve(targetName)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown_storage_target"})
		return false
	}
	if write && target.ReadOnly {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "storage_target_read_only"})
		return false
	}
	if cfg.StorageAccessPolicy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "access_policy_unavailable"})
		return false
	}
	identity := requestidentity.Extract(r, requestidentity.Headers{
		Legacy: cfg.StorageSubjectHeader,
	})
	if cfg.WorkloadVerifier != nil {
		rawToken, err := workloadauth.BearerToken(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "workload_token_required"})
			return false
		}
		principal, err := cfg.WorkloadVerifier.Verify(r.Context(), rawToken)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_workload_token"})
			return false
		}
		identity.Service = principal.Service
	} else if cfg.WorkloadAuthRequired {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "workload_auth_unavailable"})
		return false
	}
	if err := identity.ValidateActor(); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "actor_required"})
		return false
	}
	contextValues := identity.PolicyContext()
	decision, err := cfg.StorageAccessPolicy.Authorize(r.Context(), accesspolicy.Request{
		Subject: identity.Actor,
		Action:  action,
		Target:  targetName,
		Context: contextValues,
	})
	if err != nil {
		slog.Error("storage access policy failed", "error", err, "target", targetName, "action", action)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "access_policy_unavailable"})
		return false
	}
	if !decision.Allow {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access_denied", "reason": decision.Reason})
		return false
	}
	return true
}

func writeStorageResult(w http.ResponseWriter, op capability.PresignedOperation, err error) {
	if err != nil {
		writeStorageProviderError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        op.URL,
		"method":     op.Method,
		"expires_at": op.ExpiresAt,
		"headers":    op.Headers,
	})
}

func writeStorageProviderError(w http.ResponseWriter, err error) {
	slog.Error("storage signer failed", "error", err)
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "storage_provider_error"})
}

func registerUnavailableCapability(mux *http.ServeMux, role runtimecfg.Role, capabilityName string) {
	pattern := routePattern(role, capabilityName)
	if pattern == "" {
		return
	}
	mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		providerUnavailable(w, role, capabilityName)
	})
}

func providerUnavailable(w http.ResponseWriter, role runtimecfg.Role, c string) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"status":     "provider_not_configured",
		"role":       role,
		"capability": c,
		"message":    "runtime route is enabled but its external provider is not configured",
	})
}

func routePattern(role runtimecfg.Role, c string) string {
	switch role {
	case runtimecfg.RoleStorage:
		switch c {
		case "upload":
			return "POST /storage/upload"
		case "download":
			return "GET /storage/download"
		case "multipart":
			return "POST /storage/multipart"
		case "presign":
			return "POST /storage/presign"
		case "metadata":
			return "GET /storage/metadata"
		}
	case runtimecfg.RoleStreaming:
		switch c {
		case "playback":
			return "POST /streaming/playback"
		case "manifest":
			return "GET /streaming/manifest"
		case "token":
			return "POST /streaming/token"
		case "origin-select":
			return "POST /streaming/origin-select"
		}
	case runtimecfg.RoleWebSocket:
		switch c {
		case "connect":
			return "GET /websocket/connect"
		case "publish":
			return "POST /websocket/publish"
		case "subscribe":
			return "POST /websocket/subscribe"
		case "presence":
			return "GET /websocket/presence"
		case "heartbeat":
			return "POST /websocket/heartbeat"
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
