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
)

type fakeLoader struct {
	input metadata.ConfigInput
	err   error
}

func (f fakeLoader) LoadRuntimeConfig(ctx context.Context, summary metadata.ConfigSummary) (metadata.ConfigInput, error) {
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
		opManager:        fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sts"}},
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
		opManager:        fakeLoader{input: metadata.ConfigInput{ID: created.ID, AuthType: "sts"}},
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
