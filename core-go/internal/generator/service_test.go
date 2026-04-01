package generator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yaman/aws-credential-manager/core-go/internal/awssso"
	"github.com/yaman/aws-credential-manager/core-go/internal/awssts"
	"github.com/yaman/aws-credential-manager/core-go/internal/credentialsfile"
	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
	"github.com/yaman/aws-credential-manager/core-go/internal/sessioncache"
)

type fakeLoader struct {
	input       metadata.ConfigInput
	err         error
	ctxDeadline time.Time
	hasDeadline bool
}

func (f *fakeLoader) LoadRuntimeConfig(ctx context.Context, summary metadata.ConfigSummary) (metadata.ConfigInput, error) {
	if deadline, ok := ctx.Deadline(); ok {
		f.ctxDeadline = deadline
		f.hasDeadline = true
	}
	return f.input, f.err
}

type fakeWriter struct {
	profile string
	creds   credentialsfile.SessionCredentials
	err     error
}

func (f *fakeWriter) UpsertProfile(profile string, creds credentialsfile.SessionCredentials) error {
	f.profile = profile
	f.creds = creds
	return f.err
}

type fakeSTS struct {
	result awssts.SessionResult
	err    error
}

func (f fakeSTS) Generate(ctx context.Context, input metadata.ConfigInput) (awssts.SessionResult, error) {
	return f.result, f.err
}

type fakeSSO struct {
	result awssso.SessionResult
	err    error
}

func (f fakeSSO) Generate(ctx context.Context, input metadata.ConfigInput) (awssso.SessionResult, error) {
	return f.result, f.err
}

type fakeSSOPersister struct {
	summary     metadata.ConfigSummary
	session     sessioncache.Session
	called      bool
	err         error
	ctxDeadline time.Time
	hasDeadline bool
}

func (f *fakeSSOPersister) PersistSSOSessionState(ctx context.Context, summary metadata.ConfigSummary, session sessioncache.Session) error {
	if deadline, ok := ctx.Deadline(); ok {
		f.ctxDeadline = deadline
		f.hasDeadline = true
	}
	f.summary = summary
	f.session = session
	f.called = true
	return f.err
}

func TestGenerateSTSRecordsSuccess(t *testing.T) {
	store := metadata.NewStoreAt(filepath.Join(t.TempDir(), "index.json"))
	created, err := store.Create(metadata.ConfigInput{
		SettingName:            "demo",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
	})
	if err != nil {
		t.Fatal(err)
	}

	expiration := time.Now().UTC().Add(1 * time.Hour)
	writer := &fakeWriter{}
	service := &Service{
		opManager:        &fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sts"}},
		metadataStore:    store,
		credentialsStore: writer,
		stsService: fakeSTS{result: awssts.SessionResult{
			AccessKeyID:     "akid",
			SecretAccessKey: "secret",
			SessionToken:    "token",
			Expiration:      expiration,
		}},
		ssoService: fakeSSO{},
	}

	result, err := service.Generate(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if writer.profile != "demo" || writer.creds.AccessKeyID != "akid" {
		t.Fatalf("credentials writer was not called as expected: %+v %+v", writer.profile, writer.creds)
	}
	if result.Summary.LastKnownExpiration == nil || !result.Summary.LastKnownExpiration.Equal(expiration) {
		t.Fatalf("summary expiration not recorded: %+v", result.Summary.LastKnownExpiration)
	}
}

func TestGenerateRecordsFailure(t *testing.T) {
	store := metadata.NewStoreAt(filepath.Join(t.TempDir(), "index.json"))
	created, err := store.Create(metadata.ConfigInput{
		SettingName:            "demo",
		AuthType:               "sts",
		OnePasswordAccountName: "soracom",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
	})
	if err != nil {
		t.Fatal(err)
	}

	service := &Service{
		opManager:        &fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sts"}},
		metadataStore:    store,
		credentialsStore: &fakeWriter{},
		stsService:       fakeSTS{err: errors.New("boom")},
		ssoService:       fakeSSO{},
	}

	if _, err := service.Generate(context.Background(), created.ID); err == nil {
		t.Fatal("expected generate to fail")
	}
	summary, err := store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.LastErrorSummary == "" {
		t.Fatal("expected last error summary to be recorded")
	}
}

func TestGenerateSSOPersistsSessionState(t *testing.T) {
	store := metadata.NewStoreAt(filepath.Join(t.TempDir(), "index.json"))
	created, err := store.Create(metadata.ConfigInput{
		SettingName:            "demo-sso",
		AuthType:               "sso",
		OnePasswordAccountName: "account",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
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

	expiration := time.Now().UTC().Add(1 * time.Hour)
	persister := &fakeSSOPersister{}
	service := &Service{
		opManager:        &fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sso"}},
		metadataStore:    store,
		credentialsStore: &fakeWriter{},
		stsService:       fakeSTS{},
		ssoService: fakeSSO{result: awssso.SessionResult{
			AccessKeyID:     "akid",
			SecretAccessKey: "secret",
			SessionToken:    "token",
			Expiration:      expiration,
			Session: sessioncache.Session{
				AccessToken:  "access",
				RefreshToken: "refresh",
			},
		}},
		ssoPersister: persister,
	}

	if _, err := service.Generate(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if !persister.called {
		t.Fatal("expected SSO session state to be persisted")
	}
	if persister.summary.ID != created.ID {
		t.Fatalf("unexpected summary passed to persister: %+v", persister.summary)
	}
	if persister.session.RefreshToken != "refresh" {
		t.Fatalf("unexpected session passed to persister: %+v", persister.session)
	}
}

func TestGenerateUsesBoundedOnePasswordTimeoutForLoad(t *testing.T) {
	store := metadata.NewStoreAt(filepath.Join(t.TempDir(), "index.json"))
	created, err := store.Create(metadata.ConfigInput{
		SettingName:            "demo-sso",
		AuthType:               "sso",
		OnePasswordAccountName: "account",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
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

	loader := &fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sso"}}
	service := &Service{
		opManager:        loader,
		metadataStore:    store,
		credentialsStore: &fakeWriter{},
		stsService:       fakeSTS{},
		ssoService: fakeSSO{result: awssso.SessionResult{
			AccessKeyID:     "akid",
			SecretAccessKey: "secret",
			SessionToken:    "token",
			Expiration:      time.Now().UTC().Add(1 * time.Hour),
		}},
	}

	start := time.Now()
	if _, err := service.Generate(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if !loader.hasDeadline {
		t.Fatal("expected loader context to have a deadline")
	}
	remaining := loader.ctxDeadline.Sub(start)
	if remaining <= 0 || remaining > 16*time.Second {
		t.Fatalf("expected bounded onepassword timeout, got remaining=%s", remaining)
	}
}

func TestGenerateUsesBoundedOnePasswordTimeoutForPersist(t *testing.T) {
	store := metadata.NewStoreAt(filepath.Join(t.TempDir(), "index.json"))
	created, err := store.Create(metadata.ConfigInput{
		SettingName:            "demo-sso",
		AuthType:               "sso",
		OnePasswordAccountName: "account",
		ProfileName:            "demo",
		VaultID:                "vault",
		ItemID:                 "item",
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

	persister := &fakeSSOPersister{}
	service := &Service{
		opManager:        &fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sso"}},
		metadataStore:    store,
		credentialsStore: &fakeWriter{},
		stsService:       fakeSTS{},
		ssoService: fakeSSO{result: awssso.SessionResult{
			AccessKeyID:     "akid",
			SecretAccessKey: "secret",
			SessionToken:    "token",
			Expiration:      time.Now().UTC().Add(1 * time.Hour),
		}},
		ssoPersister: persister,
	}

	start := time.Now()
	if _, err := service.Generate(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if !persister.hasDeadline {
		t.Fatal("expected persister context to have a deadline")
	}
	remaining := persister.ctxDeadline.Sub(start)
	if remaining <= 0 || remaining > 16*time.Second {
		t.Fatalf("expected bounded onepassword timeout, got remaining=%s", remaining)
	}
}
