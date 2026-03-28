package settings

import "testing"

func TestNormalizeKeepsOnlySelectedOnePasswordAccount(t *testing.T) {
	settings := normalize(AppSettings{
		OnePasswordAccounts:            []string{" account-a ", "account-b", "account-a"},
		SelectedOnePasswordAccountName: "account-b",
	})

	if settings.SelectedOnePasswordAccountName != "account-b" {
		t.Fatalf("expected selected account to be preserved, got %q", settings.SelectedOnePasswordAccountName)
	}
	if len(settings.OnePasswordAccounts) != 1 || settings.OnePasswordAccounts[0] != "account-b" {
		t.Fatalf("expected accounts to collapse to the selected account, got %#v", settings.OnePasswordAccounts)
	}
}

func TestNormalizeFallsBackToFirstAccountWhenSelectedIsMissing(t *testing.T) {
	settings := normalize(AppSettings{
		OnePasswordAccounts: []string{"account-a", "account-b"},
	})

	if settings.SelectedOnePasswordAccountName != "account-a" {
		t.Fatalf("expected first account to become selected, got %q", settings.SelectedOnePasswordAccountName)
	}
	if len(settings.OnePasswordAccounts) != 1 || settings.OnePasswordAccounts[0] != "account-a" {
		t.Fatalf("expected only the selected account to remain, got %#v", settings.OnePasswordAccounts)
	}
}
