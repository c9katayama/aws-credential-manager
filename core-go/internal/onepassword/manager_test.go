package onepasswordmanager

import (
	"context"
	"strings"
	"testing"

	onepassword "github.com/1password/onepassword-sdk-go"

	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
)

func TestDecodeConfigInputHandlesPrefixedTOTPFields(t *testing.T) {
	item := onepassword.Item{
		ID:      "item-1",
		VaultID: "vault-1",
		Fields: []onepassword.ItemField{
			{
				ID:        "setting_name",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "demo",
			},
			{
				ID:        "profile_name",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "demo-profile",
			},
			{
				ID:        "auth_type",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "sts",
			},
			{
				ID:        "TOTP_mfa_totp",
				FieldType: onepassword.ItemFieldTypeTOTP,
				Value:     "otpauth://totp/example?secret=abc",
			},
		},
	}

	input := decodeConfigInput(item, "cfg-1")
	if input.MFATOTP != "otpauth://totp/example?secret=abc" {
		t.Fatalf("expected prefixed TOTP field to decode, got %q", input.MFATOTP)
	}
}

func TestMergeManagedFieldsDeduplicatesExistingTOTPEntries(t *testing.T) {
	sectionID := stsSectionID
	existing := []onepassword.ItemField{
		{
			ID:        "TOTP_mfa_totp",
			Title:     "MFA TOTP",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeTOTP,
			Value:     "otpauth://old-1",
		},
		{
			ID:        "TOTP_mfa_totp",
			Title:     "MFA TOTP",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeTOTP,
			Value:     "otpauth://old-2",
		},
	}
	replacement := []onepassword.ItemField{
		{
			ID:        "TOTP_mfa_totp",
			Title:     "MFA TOTP",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeTOTP,
			Value:     "otpauth://new",
		},
	}

	merged := mergeManagedFields(existing, replacement)
	if len(merged) != 1 {
		t.Fatalf("expected duplicate TOTP fields to be collapsed, got %d fields", len(merged))
	}
	if merged[0].Value != "otpauth://new" {
		t.Fatalf("expected replacement TOTP field to win, got %q", merged[0].Value)
	}
}

func TestResolveStoredSecretsDoesNotOverwriteTOTPURLForRuntime(t *testing.T) {
	manager := &Manager{}
	sectionID := stsSectionID
	input := metadata.ConfigInput{
		MFATOTP: "otpauth://totp/example?secret=abc",
	}
	item := onepassword.Item{
		VaultID: "vault-1",
		ID:      "item-1",
		Fields: []onepassword.ItemField{
			{
				ID:        "TOTP_mfa_totp",
				SectionID: &sectionID,
				FieldType: onepassword.ItemFieldTypeTOTP,
				Value:     "otpauth://totp/example?secret=abc",
			},
		},
	}

	if err := manager.resolveStoredSecrets(context.Background(), nil, item, &input); err != nil {
		t.Fatal(err)
	}
	if input.MFATOTP != "otpauth://totp/example?secret=abc" {
		t.Fatalf("unexpected TOTP value after stored secret resolution: %q", input.MFATOTP)
	}
}

func TestResolveStoredSecretsSkipsResolveWhenValueAlreadyPresent(t *testing.T) {
	manager := &Manager{}
	sectionID := ssoSectionID
	input := metadata.ConfigInput{
		SSOPassword: "already-present",
	}
	item := onepassword.Item{
		VaultID: "vault-1",
		ID:      "item-1",
		Fields: []onepassword.ItemField{
			{
				ID:        "sso_password",
				SectionID: &sectionID,
				FieldType: onepassword.ItemFieldTypeConcealed,
				Value:     "already-present",
			},
		},
	}

	if err := manager.resolveStoredSecrets(context.Background(), nil, item, &input); err != nil {
		t.Fatal(err)
	}
	if input.SSOPassword != "already-present" {
		t.Fatalf("unexpected password after stored secret resolution: %q", input.SSOPassword)
	}
}

func TestNewMFAFieldTreatsRawSecretAsTOTP(t *testing.T) {
	field := newMFAField(stsSectionID, canonicalTOTPFieldID("mfa_totp"), "MFA TOTP", "SXU4MUR2ABCDEFGH234567")
	if field.FieldType != onepassword.ItemFieldTypeTOTP {
		t.Fatalf("expected raw secret to become TOTP field, got %s", field.FieldType)
	}
	if !strings.HasPrefix(field.Value, "otpauth://totp/") {
		t.Fatalf("expected raw secret to be converted into otpauth URI, got %q", field.Value)
	}
}

func TestMergeManagedFieldsAllowsClearingNonSecretFields(t *testing.T) {
	sectionID := stsSectionID
	existing := []onepassword.ItemField{
		{
			ID:        "role_arn",
			Title:     "Role ARN",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeText,
			Value:     "arn:aws:iam::123456789012:role/demo",
		},
	}
	replacement := []onepassword.ItemField{
		{
			ID:        "mfa_arn",
			Title:     "MFA ARN",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeText,
			Value:     "arn:aws:iam::123456789012:mfa/demo",
		},
	}

	merged := mergeManagedFields(existing, replacement)
	if len(merged) != 1 {
		t.Fatalf("expected cleared non-secret field to be removed, got %d fields", len(merged))
	}
	if merged[0].ID != "mfa_arn" {
		t.Fatalf("expected remaining field to be mfa_arn, got %q", merged[0].ID)
	}
}

func TestMergeManagedFieldsPreservesSecretFieldsWhenOmitted(t *testing.T) {
	sectionID := stsSectionID
	existing := []onepassword.ItemField{
		{
			ID:        "aws_secret_access_key",
			Title:     "AWS Secret Access Key",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeConcealed,
			Value:     "secret",
		},
	}
	replacement := []onepassword.ItemField{
		{
			ID:        "mfa_arn",
			Title:     "MFA ARN",
			SectionID: &sectionID,
			FieldType: onepassword.ItemFieldTypeText,
			Value:     "arn:aws:iam::123456789012:mfa/demo",
		},
	}

	merged := mergeManagedFields(existing, replacement)
	if len(merged) != 2 {
		t.Fatalf("expected omitted secret field to be preserved, got %d fields", len(merged))
	}
}

func TestDecodeConfigInputFallsBackToGenericFieldTitles(t *testing.T) {
	item := onepassword.Item{
		ID:      "item-1",
		VaultID: "vault-1",
		Title:   "org-yama",
		Fields: []onepassword.ItemField{
			{
				ID:        "profile",
				Title:     "Profile Name",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "org-yama",
			},
			{
				ID:        "access",
				Title:     "AWS Access Key ID",
				FieldType: onepassword.ItemFieldTypeConcealed,
				Value:     "AKIAEXAMPLE",
			},
			{
				ID:        "secret",
				Title:     "AWS Secret Access Key",
				FieldType: onepassword.ItemFieldTypeConcealed,
				Value:     "secret-value",
			},
			{
				ID:        "mfa",
				Title:     "MFA ARN",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "arn:aws:iam::123456789012:mfa/demo",
			},
		},
	}

	input := decodeConfigInput(item, "cfg-1")
	if input.SettingName != "org-yama" {
		t.Fatalf("expected title fallback for setting name, got %q", input.SettingName)
	}
	if input.ProfileName != "org-yama" {
		t.Fatalf("expected profile name fallback, got %q", input.ProfileName)
	}
	if input.AuthType != "sts" {
		t.Fatalf("expected auth type inference to sts, got %q", input.AuthType)
	}
	if input.AWSAccessKeyID != "AKIAEXAMPLE" {
		t.Fatalf("expected access key fallback, got %q", input.AWSAccessKeyID)
	}
}
