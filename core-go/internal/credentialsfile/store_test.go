package credentialsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertProfileCreatesAndUpdatesTargetProfile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "credentials")
	if err := os.WriteFile(path, []byte("[default]\naws_access_key_id=old\naws_secret_access_key=old\naws_session_token=old\n\n[other]\nfoo=bar\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpsertProfile("default", SessionCredentials{
		AccessKeyID:     "new-akid",
		SecretAccessKey: "new-secret",
		SessionToken:    "new-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "[default]\naws_access_key_id=new-akid\naws_secret_access_key=new-secret\naws_session_token=new-token\n") {
		t.Fatalf("updated profile missing from credentials file:\n%s", text)
	}
	if !strings.Contains(text, "[other]\nfoo=bar\n") {
		t.Fatalf("unrelated profile should be preserved:\n%s", text)
	}
}

func TestUpsertProfileAppendsMissingProfile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "credentials")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpsertProfile("sandbox", SessionCredentials{
		AccessKeyID:     "akid",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	})
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "[sandbox]\naws_access_key_id=akid\naws_secret_access_key=secret\naws_session_token=token\n") {
		t.Fatalf("expected appended profile, got:\n%s", text)
	}
}
