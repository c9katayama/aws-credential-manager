package onepasswordmanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestResolveStoredSecretsReturnsResolutionError(t *testing.T) {
	sectionID := ssoSectionID
	manager := &Manager{
		fieldResolver: func(ctx context.Context, client *onepassword.Client, vaultID, itemID, sectionID, fieldID, attribute string) (string, error) {
			return "", errors.New("connection was unexpectedly dropped by the desktop app")
		},
	}
	input := metadata.ConfigInput{}
	item := onepassword.Item{
		VaultID: "vault-1",
		ID:      "item-1",
		Fields: []onepassword.ItemField{
			{
				ID:        "sso_password",
				SectionID: &sectionID,
				FieldType: onepassword.ItemFieldTypeConcealed,
			},
		},
	}

	err := manager.resolveStoredSecrets(context.Background(), nil, item, &input)
	if err == nil {
		t.Fatal("expected stored secret resolution error to be returned")
	}
	if !strings.Contains(err.Error(), "unexpectedly dropped") {
		t.Fatalf("unexpected error: %v", err)
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

func TestDecodeConfigInputDefaultsSSOLoginMethodForLegacyItems(t *testing.T) {
	item := onepassword.Item{
		ID:      "item-1",
		VaultID: "vault-1",
		Fields: []onepassword.ItemField{
			{
				ID:        "auth_type",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "sso",
			},
			{
				ID:        "sso_start_url",
				FieldType: onepassword.ItemFieldTypeURL,
				Value:     "https://example.awsapps.com/start",
			},
			{
				ID:        "sso_region",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "ap-northeast-1",
			},
			{
				ID:        "sso_account_id",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "123456789012",
			},
			{
				ID:        "sso_role_name",
				FieldType: onepassword.ItemFieldTypeText,
				Value:     "AdministratorAccess",
			},
		},
	}

	input := decodeConfigInput(item, "cfg-legacy")
	if input.SSOLoginMethod != "deviceCode" {
		t.Fatalf("expected legacy sso item to default to deviceCode, got %q", input.SSOLoginMethod)
	}
}

func TestShouldRetryClientErrorWhenDesktopAppDropsConnection(t *testing.T) {
	err := shouldRetryClientError(errors.New("connection was unexpectedly dropped by the desktop app"))
	if !err {
		t.Fatal("expected dropped desktop app connection to be retryable")
	}
}

func TestShouldRetryClientErrorWhenDesktopAppIsUnavailable(t *testing.T) {
	err := shouldRetryClientError(errors.New("1Password desktop application not found"))
	if !err {
		t.Fatal("expected desktop application lookup failure to be retryable")
	}
}

func TestShouldRetryClientErrorWhenDarwinInitReturnsRetryableCode(t *testing.T) {
	err := shouldRetryClientError(errors.New(
		"error initializing client: an internal error occurred. Please contact 1Password support and mention the return code: -6",
	))
	if !err {
		t.Fatal("expected darwin init return code -6 to be retryable")
	}
}

func TestStatusRetriesWhenCachedDesktopConnectionDrops(t *testing.T) {
	attempts := 0
	manager := &Manager{
		clientFactory: func(ctx context.Context, accountName string) (*onepassword.Client, error) {
			attempts++
			return &onepassword.Client{}, nil
		},
		connectionProbe: func(ctx context.Context, client *onepassword.Client) error {
			if attempts == 1 {
				return errors.New("connection was unexpectedly dropped by the desktop app")
			}
			return nil
		},
	}

	status := manager.Status(context.Background(), "demo-account")
	if !status.Connected {
		t.Fatalf("expected status check to recover from dropped connection, got %#v", status)
	}
	if attempts != 2 {
		t.Fatalf("expected client to be recreated after dropped connection, got %d creations", attempts)
	}
}

func TestStatusRetriesWhenDesktopAppBecomesAvailableDuringInitialization(t *testing.T) {
	attempts := 0
	manager := &Manager{
		clientFactory: func(ctx context.Context, accountName string) (*onepassword.Client, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("1Password desktop application not found")
			}
			return &onepassword.Client{}, nil
		},
		connectionProbe: func(ctx context.Context, client *onepassword.Client) error {
			return nil
		},
	}

	status := manager.Status(context.Background(), "demo-account")
	if !status.Connected {
		t.Fatalf("expected status check to recover once desktop app becomes available, got %#v", status)
	}
	if attempts != 2 {
		t.Fatalf("expected client initialization to retry, got %d attempts", attempts)
	}
}

func TestStatusRetriesWhenDarwinInitReturnsRetryableCode(t *testing.T) {
	attempts := 0
	manager := &Manager{
		clientFactory: func(ctx context.Context, accountName string) (*onepassword.Client, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("error initializing client: an internal error occurred. Please contact 1Password support and mention the return code: -6")
			}
			return &onepassword.Client{}, nil
		},
		connectionProbe: func(ctx context.Context, client *onepassword.Client) error {
			return nil
		},
	}

	status := manager.Status(context.Background(), "demo-account")
	if !status.Connected {
		t.Fatalf("expected status check to recover from darwin init return code -6, got %#v", status)
	}
	if attempts != 2 {
		t.Fatalf("expected client initialization to retry after darwin return code -6, got %d attempts", attempts)
	}
}

func TestStatusTreatsVaultListAccessAsConnected(t *testing.T) {
	manager := &Manager{
		clientFactory: func(ctx context.Context, accountName string) (*onepassword.Client, error) {
			return &onepassword.Client{}, nil
		},
		vaultLister: func(ctx context.Context, client *onepassword.Client) ([]onepassword.VaultOverview, error) {
			return []onepassword.VaultOverview{
				{ID: "vault-1", Title: "Shared"},
			}, nil
		},
	}

	status := manager.Status(context.Background(), "demo-account")
	if !status.Connected {
		t.Fatalf("expected status check to succeed when vault listing is available, got %#v", status)
	}
}

func TestPrivateVaultStillPrefersPersonalVaultNames(t *testing.T) {
	manager := &Manager{
		vaultLister: func(ctx context.Context, client *onepassword.Client) ([]onepassword.VaultOverview, error) {
			return []onepassword.VaultOverview{
				{ID: "vault-1", Title: "Shared"},
				{ID: "vault-2", Title: "Private"},
			}, nil
		},
	}

	vault, err := manager.privateVault(context.Background(), &onepassword.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if vault.ID != "vault-2" {
		t.Fatalf("expected private vault to be selected, got %#v", vault)
	}
}

func TestClientForAccountRecreatesIdleClient(t *testing.T) {
	attempts := 0
	manager := &Manager{
		clientFactory: func(ctx context.Context, accountName string) (*onepassword.Client, error) {
			attempts++
			return &onepassword.Client{}, nil
		},
	}

	first, err := manager.clientForAccount(context.Background(), "demo-account")
	if err != nil {
		t.Fatal(err)
	}

	manager.mu.Lock()
	manager.lastClientUse = time.Now().Add(-clientMaxIdle - time.Second)
	manager.mu.Unlock()

	second, err := manager.clientForAccount(context.Background(), "demo-account")
	if err != nil {
		t.Fatal(err)
	}

	if attempts != 2 {
		t.Fatalf("expected idle client to be recreated, got %d creations", attempts)
	}
	if first == second {
		t.Fatal("expected a new client instance after the idle window elapsed")
	}
}

func TestWithClientRetryHonorsContextWhileWaitingForSDKLock(t *testing.T) {
	manager := &Manager{
		clientFactory: func(ctx context.Context, accountName string) (*onepassword.Client, error) {
			return &onepassword.Client{}, nil
		},
	}

	gate := manager.sdkLockChannel()
	<-gate
	defer func() {
		gate <- struct{}{}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	called := false
	_, err := withClientRetry(ctx, manager, "demo-account", func(client *onepassword.Client) (struct{}, error) {
		called = true
		return struct{}{}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded while waiting for SDK lock, got %v", err)
	}
	if called {
		t.Fatal("expected operation not to run when the SDK lock could not be acquired in time")
	}
}
