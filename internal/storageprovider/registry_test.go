package storageprovider

import (
	"context"
	"testing"

	"gateway-runtime/internal/capability"
)

type fakeSigner struct{}

func (fakeSigner) PresignPut(context.Context, capability.PresignedRequest) (capability.PresignedOperation, error) {
	return capability.PresignedOperation{}, nil
}
func (fakeSigner) PresignGet(context.Context, capability.PresignedRequest) (capability.PresignedOperation, error) {
	return capability.PresignedOperation{}, nil
}

func TestRegistryResolve(t *testing.T) {
	r := Registry{"minio-main": {Signer: fakeSigner{}}}
	if _, ok := r.Resolve("minio-main"); !ok {
		t.Fatal("provider not resolved")
	}
}

func TestValidateRequiredRejectsMissing(t *testing.T) {
	r := Registry{"minio-main": {Signer: fakeSigner{}}}
	if err := r.ValidateRequired("missing"); err == nil {
		t.Fatal("expected missing provider error")
	}
}
