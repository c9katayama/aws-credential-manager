package awssts

import (
	"testing"

	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
)

func TestValidateRuntimeStaticCredentialsRejectsNonASCII(t *testing.T) {
	err := validateRuntimeStaticCredentials("TESTACCESSKEY123Ą", "secret")
	if err == nil {
		t.Fatal("expected invalid access key to be rejected")
	}
}

func TestValidateRuntimeStaticCredentialsRejectsWhitespaceSecret(t *testing.T) {
	err := validateRuntimeStaticCredentials("TESTACCESSKEY1234", "secret ")
	if err == nil {
		t.Fatal("expected secret with trailing whitespace to be rejected")
	}
}

func TestServiceRejectsInvalidRoleARNBeforeCallingAWS(t *testing.T) {
	service := New()
	_, err := service.Generate(t.Context(), metadata.ConfigInput{
		AuthType:           "sts",
		AWSAccessKeyID:     "TESTACCESSKEY1234",
		AWSSecretAccessKey: "not-a-real-secret-key-for-tests-1234567890",
		MFATOTP:            "123456",
		RoleArn:            "otpauth://totp/example?secret=abc",
	})
	if err == nil {
		t.Fatal("expected invalid role ARN to be rejected")
	}
}
