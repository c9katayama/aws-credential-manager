package metadata

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{path: filepath.Join(t.TempDir(), "index.json")}
}

func TestCreateUpdateAndRecordResult(t *testing.T) {
	store := newTestStore(t)

	created, err := store.Create(ConfigInput{
		SettingName:            "dev-admin",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "dev-admin",
		VaultID:                "vault",
		ItemID:                 "item",
		AutoRefreshEnabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("expected generated id")
	}

	updated, err := store.Update(ConfigInput{
		ID:                     created.ID,
		SettingName:            "dev-admin-updated",
		AuthType:               "sso",
		OnePasswordAccountName: "soracom",
		ProfileName:            "sandbox-admin",
		VaultID:                "vault-2",
		ItemID:                 "item-2",
		AutoRefreshEnabled:     false,
		SSOStartURL:            "https://example.awsapps.com/start",
		SSORegion:              "ap-northeast-1",
		SSOUsername:            "demo@example.com",
		SSOPassword:            "password",
		SSOMFATOTP:             "otpauth://totp/example?secret=ABCDEF1234567890",
		SSOAccountID:           "123456789012",
		SSORoleName:            "AdministratorAccess",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SettingName != "dev-admin-updated" || updated.AuthType != "sso" {
		t.Fatalf("unexpected updated summary: %+v", updated)
	}

	expiration := time.Now().UTC().Add(30 * time.Minute)
	recorded, err := store.RecordResult(created.ID, &expiration, "")
	if err != nil {
		t.Fatal(err)
	}
	if recorded.LastKnownExpiration == nil || !recorded.LastKnownExpiration.Equal(expiration) {
		t.Fatalf("expiration was not recorded: %+v", recorded.LastKnownExpiration)
	}
	if recorded.LastRefreshTime == nil {
		t.Fatal("expected last refresh time")
	}
}

func TestCreateRejectsInvalidSTSAccessKey(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Create(ConfigInput{
		SettingName:            "demo",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
		AWSAccessKeyID:         "AKIAJVVEIEFWZ4IKGL3Ą",
		AWSSecretAccessKey:     "secret",
	})
	if err == nil {
		t.Fatal("expected invalid access key to be rejected")
	}
}

func TestCreateAllowsBrowserPKCESSOWithoutLegacyCredentials(t *testing.T) {
	store := newTestStore(t)

	created, err := store.Create(ConfigInput{
		SettingName:            "sandbox-sso",
		AuthType:               "sso",
		OnePasswordAccountName: "soracom",
		ProfileName:            "sandbox-sso",
		VaultID:                "vault",
		ItemID:                 "item",
		SSOStartURL:            "https://example.awsapps.com/start",
		SSOIssuerURL:           "https://d-abc123.awsapps.com/start",
		SSORegion:              "ap-northeast-1",
		SSOLoginMethod:         "browserPkce",
		SSOAccountID:           "123456789012",
		SSORoleName:            "AdministratorAccess",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthType != "sso" {
		t.Fatalf("expected sso summary, got %+v", created)
	}
}

func TestCreateRejectsInvalidSSOLoginMethod(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Create(ConfigInput{
		SettingName:            "sandbox-sso",
		AuthType:               "sso",
		OnePasswordAccountName: "soracom",
		ProfileName:            "sandbox-sso",
		VaultID:                "vault",
		ItemID:                 "item",
		SSOStartURL:            "https://example.awsapps.com/start",
		SSORegion:              "ap-northeast-1",
		SSOLoginMethod:         "browserOnly",
		SSOAccountID:           "123456789012",
		SSORoleName:            "AdministratorAccess",
	})
	if err == nil {
		t.Fatal("expected invalid ssoLoginMethod to be rejected")
	}
}

func TestSyncManagedSummariesPreservesRuntimeState(t *testing.T) {
	store := newTestStore(t)

	created, err := store.Create(ConfigInput{
		SettingName:            "demo",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
		AutoRefreshEnabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	expiration := time.Now().UTC().Add(10 * time.Minute)
	if _, err := store.RecordResult(created.ID, &expiration, ""); err != nil {
		t.Fatal(err)
	}

	index, err := store.SyncManagedSummaries([]ConfigSummary{{
		SettingName:            "demo-renamed",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "demo-profile",
		VaultID:                "vault",
		ItemID:                 "item",
		AutoRefreshEnabled:     false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Configs) != 1 {
		t.Fatalf("expected one synced config, got %d", len(index.Configs))
	}
	if index.Configs[0].ID != created.ID {
		t.Fatalf("expected existing ID to be preserved, got %q", index.Configs[0].ID)
	}
	if index.Configs[0].LastKnownExpiration == nil || !index.Configs[0].LastKnownExpiration.Equal(expiration) {
		t.Fatalf("expected runtime state to be preserved, got %+v", index.Configs[0].LastKnownExpiration)
	}
	if index.Configs[0].SettingName != "demo-renamed" {
		t.Fatalf("expected synced fields to replace local summary, got %+v", index.Configs[0])
	}
}

func TestClearErrorSummariesPreservesOtherRuntimeState(t *testing.T) {
	store := newTestStore(t)

	created, err := store.Create(ConfigInput{
		SettingName:            "demo",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
		AutoRefreshEnabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	expiration := time.Now().UTC().Add(10 * time.Minute)
	recorded, err := store.RecordResult(created.ID, &expiration, "boom")
	if err != nil {
		t.Fatal(err)
	}
	if recorded.LastErrorSummary == "" {
		t.Fatal("expected test fixture to contain an error summary")
	}

	if err := store.ClearErrorSummaries(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LastErrorSummary != "" {
		t.Fatalf("expected error summary to be cleared, got %q", reloaded.LastErrorSummary)
	}
	if reloaded.LastKnownExpiration == nil || !reloaded.LastKnownExpiration.Equal(expiration) {
		t.Fatalf("expected expiration to be preserved, got %+v", reloaded.LastKnownExpiration)
	}
	if reloaded.LastRefreshTime == nil {
		t.Fatal("expected last refresh time to be preserved")
	}
}
