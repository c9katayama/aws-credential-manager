package awssso

import (
	"errors"
	"testing"

	"github.com/aws/smithy-go"
)

func TestIsInvalidClientErrorMatchesAWSAPIErrorCode(t *testing.T) {
	err := &smithy.GenericAPIError{
		Code:    "InvalidClientException",
		Message: "invalid client",
	}

	if !isInvalidClientError(err) {
		t.Fatal("expected InvalidClientException to be treated as a stale SSO client registration")
	}
}

func TestIsInvalidClientErrorIgnoresOtherErrors(t *testing.T) {
	if isInvalidClientError(errors.New("boom")) {
		t.Fatal("expected non-API errors to be ignored")
	}

	err := &smithy.GenericAPIError{
		Code:    "InvalidGrantException",
		Message: "invalid grant",
	}
	if isInvalidClientError(err) {
		t.Fatal("expected non-client API errors to be ignored")
	}
}

func TestNormalizeLoginMethodDefaultsToDeviceCode(t *testing.T) {
	if method := normalizeLoginMethod(""); method != deviceCodeLoginMethod {
		t.Fatalf("expected empty method to default to deviceCode, got %q", method)
	}
	if method := normalizeLoginMethod("legacy"); method != deviceCodeLoginMethod {
		t.Fatalf("expected unknown method to default to deviceCode, got %q", method)
	}
}

func TestNormalizeLoginMethodPreservesBrowserPKCE(t *testing.T) {
	if method := normalizeLoginMethod(browserPKCELoginMethod); method != browserPKCELoginMethod {
		t.Fatalf("expected browserPkce to be preserved, got %q", method)
	}
}
