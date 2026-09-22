package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-runtime/internal/capability"
	"gateway-runtime/internal/outbound"
)

func TestRemoteSignerPresignPut(t *testing.T) {
	var gotPath string
	var got presignPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(presignResponse{
			URL:       "https://object.example/upload",
			Method:    "PUT",
			ExpiresAt: time.Unix(1700000000, 0).UTC(),
			Headers:   map[string]string{"Content-Type": "application/pdf"},
		})
	}))
	defer srv.Close()

	client, err := outbound.New(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	signer := RemoteSigner{HTTP: client}
	op, err := signer.PresignPut(context.Background(), capability.PresignedRequest{
		Bucket:      "assets",
		ObjectKey:   "P1/a.pdf",
		ContentType: "application/pdf",
		ExpiresIn:   2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/storage/presign/put" {
		t.Fatalf("path=%q", gotPath)
	}
	if got.Bucket != "assets" || got.ObjectKey != "P1/a.pdf" || got.ExpiresIn != 120 {
		t.Fatalf("payload=%+v", got)
	}
	if op.URL != "https://object.example/upload" || op.Method != "PUT" {
		t.Fatalf("op=%+v", op)
	}
}

func TestRemoteSignerMultipartLifecycle(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/storage/multipart/initiate":
			_ = json.NewEncoder(w).Encode(multipartInitiateResponse{UploadID: "u1", Bucket: "assets", ObjectKey: "big.bin", ExpiresAt: time.Now().Add(time.Hour)})
		case "/api/storage/multipart/part":
			_ = json.NewEncoder(w).Encode(presignResponse{URL: "https://object.example/part", Method: "PUT", ExpiresAt: time.Now().Add(time.Minute)})
		case "/api/storage/multipart/complete":
			_ = json.NewEncoder(w).Encode(multipartCompleteResponse{ETag: "final", VersionID: "v1"})
		case "/api/storage/multipart/abort":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := outbound.New(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	signer := RemoteSigner{HTTP: client}
	up, err := signer.InitiateMultipart(context.Background(), capability.MultipartInitiateRequest{Bucket: "assets", ObjectKey: "big.bin", ExpiresIn: time.Minute})
	if err != nil || up.UploadID != "u1" {
		t.Fatalf("initiate=%+v err=%v", up, err)
	}
	op, err := signer.PresignMultipartPart(context.Background(), capability.MultipartPartRequest{Bucket: "assets", ObjectKey: "big.bin", UploadID: "u1", PartNumber: 1, ExpiresIn: time.Minute})
	if err != nil || op.Method != "PUT" {
		t.Fatalf("part=%+v err=%v", op, err)
	}
	done, err := signer.CompleteMultipart(context.Background(), capability.MultipartCompleteRequest{Bucket: "assets", ObjectKey: "big.bin", UploadID: "u1", Parts: []capability.CompletedPart{{PartNumber: 1, ETag: "e1"}}})
	if err != nil || done.ETag != "final" {
		t.Fatalf("complete=%+v err=%v", done, err)
	}
	if err := signer.AbortMultipart(context.Background(), capability.MultipartAbortRequest{Bucket: "assets", ObjectKey: "big.bin", UploadID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestRemoteSignerMetadata(t *testing.T) {
	var gotPath string
	var got metadataPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(capability.StorageMetadata{
			Exists:      true,
			Bucket:      "assets",
			ObjectKey:   "P1/a.pdf",
			Size:        1234,
			ContentType: "application/pdf",
			ETag:        "etag-1",
			Checksums:   map[string]string{"sha256": "abc"},
		})
	}))
	defer srv.Close()

	client, err := outbound.New(srv.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	signer := RemoteSigner{HTTP: client}
	out, err := signer.GetMetadata(context.Background(), capability.StorageMetadataRequest{Bucket: "assets", ObjectKey: "P1/a.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/storage/metadata" {
		t.Fatalf("path=%q", gotPath)
	}
	if got.Bucket != "assets" || got.ObjectKey != "P1/a.pdf" {
		t.Fatalf("payload=%+v", got)
	}
	if !out.Exists || out.Size != 1234 || out.Checksums["sha256"] != "abc" {
		t.Fatalf("out=%+v", out)
	}
}
