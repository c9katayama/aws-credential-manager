package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const CurrentSchemaVersion = 1

type AppSettings struct {
	SchemaVersion                  int      `json:"schemaVersion"`
	OnePasswordAccounts            []string `json:"onePasswordAccounts,omitempty"`
	SelectedOnePasswordAccountName string   `json:"selectedOnePasswordAccountName,omitempty"`
	OnePasswordAccountName         string   `json:"onePasswordAccountName,omitempty"`
	OnePasswordAccountConfigured   bool     `json:"onePasswordAccountConfigured,omitempty"`
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	return &Store{
		path: filepath.Join(configDir, "aws-credential-manager", "settings.json"),
	}, nil
}

func (s *Store) Load() (AppSettings, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return AppSettings{SchemaVersion: CurrentSchemaVersion}, nil
	}
	if err != nil {
		return AppSettings{}, err
	}

	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return AppSettings{}, err
	}
	if settings.SchemaVersion == 0 {
		settings.SchemaVersion = CurrentSchemaVersion
	}
	settings = normalize(settings)
	return settings, nil
}

func (s *Store) Save(settings AppSettings) error {
	settings = normalize(settings)
	if settings.SchemaVersion == 0 {
		settings.SchemaVersion = CurrentSchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, s.path)
}

func (s *Store) Ensure() (AppSettings, error) {
	settings, err := s.Load()
	if err != nil {
		return AppSettings{}, err
	}
	if err := s.Save(settings); err != nil {
		return AppSettings{}, err
	}
	return settings, nil
}

func (s *Store) Path() string {
	return s.path
}

func normalize(settings AppSettings) AppSettings {
	if settings.SchemaVersion == 0 {
		settings.SchemaVersion = CurrentSchemaVersion
	}

	if len(settings.OnePasswordAccounts) == 0 && settings.OnePasswordAccountConfigured && settings.OnePasswordAccountName != "" {
		settings.OnePasswordAccounts = []string{settings.OnePasswordAccountName}
	}

	seen := map[string]bool{}
	accounts := make([]string, 0, len(settings.OnePasswordAccounts))
	for _, account := range settings.OnePasswordAccounts {
		trimmed := account
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		accounts = append(accounts, trimmed)
	}
	settings.OnePasswordAccounts = accounts

	if settings.SelectedOnePasswordAccountName == "" && len(settings.OnePasswordAccounts) > 0 {
		settings.SelectedOnePasswordAccountName = settings.OnePasswordAccounts[0]
	}
	if settings.SelectedOnePasswordAccountName != "" && !seen[settings.SelectedOnePasswordAccountName] {
		if len(settings.OnePasswordAccounts) > 0 {
			settings.SelectedOnePasswordAccountName = settings.OnePasswordAccounts[0]
		} else {
			settings.SelectedOnePasswordAccountName = ""
		}
	}

	settings.OnePasswordAccountName = ""
	settings.OnePasswordAccountConfigured = false
	return settings
}
