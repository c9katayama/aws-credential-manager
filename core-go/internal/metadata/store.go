package metadata

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const CurrentSchemaVersion = 1

var awsAccessKeyPattern = regexp.MustCompile(`^[A-Z0-9]{16,128}$`)

type ConfigSummary struct {
	ID                       string     `json:"id"`
	SettingName              string     `json:"settingName"`
	AuthType                 string     `json:"authType"`
	OnePasswordAccountName   string     `json:"onePasswordAccountName"`
	ProfileName              string     `json:"profileName"`
	VaultID                  string     `json:"vaultID"`
	ItemID                   string     `json:"itemID"`
	AutoRefreshEnabled       bool       `json:"autoRefreshEnabled"`
	LastKnownExpiration      *time.Time `json:"lastKnownExpiration,omitempty"`
	LastRefreshTime          *time.Time `json:"lastRefreshTime,omitempty"`
	LastErrorSummary         string     `json:"lastErrorSummary,omitempty"`
	SSORefreshTokenAvailable bool       `json:"ssoRefreshTokenAvailable,omitempty"`
	SSOSessionExpiry         *time.Time `json:"ssoSessionExpiry,omitempty"`
}

type Index struct {
	SchemaVersion int             `json:"schemaVersion"`
	Configs       []ConfigSummary `json:"configs"`
}

type ConfigInput struct {
	ID                     string `json:"id,omitempty"`
	SettingName            string `json:"settingName"`
	AuthType               string `json:"authType"`
	OnePasswordAccountName string `json:"onePasswordAccountName"`
	ProfileName            string `json:"profileName"`
	VaultID                string `json:"vaultID"`
	ItemID                 string `json:"itemID"`
	AutoRefreshEnabled     bool   `json:"autoRefreshEnabled"`
	AWSAccessKeyID         string `json:"awsAccessKeyId,omitempty"`
	AWSSecretAccessKey     string `json:"awsSecretAccessKey,omitempty"`
	MFAArn                 string `json:"mfaArn,omitempty"`
	MFATOTP                string `json:"mfaTotp,omitempty"`
	RoleArn                string `json:"roleArn,omitempty"`
	RoleSessionName        string `json:"roleSessionName,omitempty"`
	ExternalID             string `json:"externalId,omitempty"`
	SessionDuration        string `json:"sessionDuration,omitempty"`
	STSRegion              string `json:"stsRegion,omitempty"`
	SSOStartURL            string `json:"ssoStartUrl,omitempty"`
	SSOIssuerURL           string `json:"ssoIssuerUrl,omitempty"`
	SSORegion              string `json:"ssoRegion,omitempty"`
	SSOUsername            string `json:"ssoUsername,omitempty"`
	SSOPassword            string `json:"ssoPassword,omitempty"`
	SSOMFATOTP             string `json:"ssoMfaTotp,omitempty"`
	SSOAccountID           string `json:"ssoAccountId,omitempty"`
	SSORoleName            string `json:"ssoRoleName,omitempty"`
	SSOAccessToken         string `json:"ssoAccessToken,omitempty"`
	SSOAccessExpiry        string `json:"ssoAccessExpiry,omitempty"`
	SSORefreshToken        string `json:"ssoRefreshToken,omitempty"`
	SSOClientID            string `json:"ssoClientId,omitempty"`
	SSOClientSecret        string `json:"ssoClientSecret,omitempty"`
	SSOClientSecretExpiry  string `json:"ssoClientSecretExpiry,omitempty"`
	SSOLastBrowserURL      string `json:"ssoLastBrowserUrl,omitempty"`
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	return NewStoreAt(filepath.Join(configDir, "aws-credential-manager", "index.json")), nil
}

func NewStoreAt(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() (Index, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Index{SchemaVersion: CurrentSchemaVersion, Configs: []ConfigSummary{}}, nil
	}
	if err != nil {
		return Index{}, err
	}

	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, err
	}
	if index.SchemaVersion == 0 {
		index.SchemaVersion = CurrentSchemaVersion
	}
	return index, nil
}

func (s *Store) Ensure() (Index, error) {
	index, err := s.Load()
	if err != nil {
		return Index{}, err
	}
	if index.SchemaVersion != CurrentSchemaVersion {
		return Index{}, errors.New("unsupported metadata schema version")
	}
	if err := s.Save(index); err != nil {
		return Index{}, err
	}
	return index, nil
}

func (s *Store) Save(index Index) error {
	if index.SchemaVersion == 0 {
		index.SchemaVersion = CurrentSchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, s.path)
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Upsert(input ConfigInput) (ConfigSummary, error) {
	if input.ID == "" {
		return s.Create(input)
	}
	return s.Update(input)
}

func (s *Store) Create(input ConfigInput) (ConfigSummary, error) {
	index, err := s.Load()
	if err != nil {
		return ConfigSummary{}, err
	}
	if index.SchemaVersion == 0 {
		index.SchemaVersion = CurrentSchemaVersion
	}

	if err := validateInput(input); err != nil {
		return ConfigSummary{}, err
	}
	input.ID = ""

	summary := ConfigSummary{
		ID:                     "",
		SettingName:            input.SettingName,
		AuthType:               input.AuthType,
		OnePasswordAccountName: input.OnePasswordAccountName,
		ProfileName:            input.ProfileName,
		VaultID:                input.VaultID,
		ItemID:                 input.ItemID,
		AutoRefreshEnabled:     input.AutoRefreshEnabled,
		LastRefreshTime:        nil,
		LastKnownExpiration:    nil,
	}

	id, err := newID()
	if err != nil {
		return ConfigSummary{}, err
	}
	summary.ID = id
	index.Configs = append(index.Configs, summary)
	slices.SortFunc(index.Configs, func(a, b ConfigSummary) int {
		return compareStrings(a.SettingName, b.SettingName)
	})
	if err := s.Save(index); err != nil {
		return ConfigSummary{}, err
	}
	return summary, nil
}

func (s *Store) Update(input ConfigInput) (ConfigSummary, error) {
	if input.ID == "" {
		return ConfigSummary{}, errors.New("id is required")
	}

	index, err := s.Load()
	if err != nil {
		return ConfigSummary{}, err
	}
	if index.SchemaVersion == 0 {
		index.SchemaVersion = CurrentSchemaVersion
	}
	if err := validateInput(input); err != nil {
		return ConfigSummary{}, err
	}

	for i, existing := range index.Configs {
		if existing.ID != input.ID {
			continue
		}

		summary := ConfigSummary{
			ID:                     input.ID,
			SettingName:            input.SettingName,
			AuthType:               input.AuthType,
			OnePasswordAccountName: input.OnePasswordAccountName,
			ProfileName:            input.ProfileName,
			VaultID:                input.VaultID,
			ItemID:                 input.ItemID,
			AutoRefreshEnabled:     input.AutoRefreshEnabled,
			LastKnownExpiration:    existing.LastKnownExpiration,
			LastRefreshTime:        existing.LastRefreshTime,
			LastErrorSummary:       existing.LastErrorSummary,
		}
		index.Configs[i] = summary
		if err := s.Save(index); err != nil {
			return ConfigSummary{}, err
		}
		return summary, nil
	}

	return ConfigSummary{}, fmt.Errorf("config not found: %s", input.ID)
}

func (s *Store) Delete(id string) error {
	if id == "" {
		return errors.New("id is required")
	}

	index, err := s.Load()
	if err != nil {
		return err
	}

	next := make([]ConfigSummary, 0, len(index.Configs))
	found := false
	for _, existing := range index.Configs {
		if existing.ID == id {
			found = true
			continue
		}
		next = append(next, existing)
	}
	if !found {
		return fmt.Errorf("config not found: %s", id)
	}

	index.Configs = next
	return s.Save(index)
}

func (s *Store) Get(id string) (ConfigSummary, error) {
	if id == "" {
		return ConfigSummary{}, errors.New("id is required")
	}

	index, err := s.Load()
	if err != nil {
		return ConfigSummary{}, err
	}

	for _, existing := range index.Configs {
		if existing.ID == id {
			return existing, nil
		}
	}

	return ConfigSummary{}, fmt.Errorf("config not found: %s", id)
}

func (s *Store) SyncManagedSummaries(incoming []ConfigSummary) (Index, error) {
	index, err := s.Load()
	if err != nil {
		return Index{}, err
	}
	if index.SchemaVersion == 0 {
		index.SchemaVersion = CurrentSchemaVersion
	}

	existingByItemKey := map[string]ConfigSummary{}
	for _, existing := range index.Configs {
		existingByItemKey[itemKey(existing.VaultID, existing.ItemID)] = existing
	}

	next := make([]ConfigSummary, 0, len(incoming))
	for _, summary := range incoming {
		key := itemKey(summary.VaultID, summary.ItemID)
		if existing, ok := existingByItemKey[key]; ok {
			summary.ID = existing.ID
			summary.LastKnownExpiration = existing.LastKnownExpiration
			summary.LastRefreshTime = existing.LastRefreshTime
			summary.LastErrorSummary = existing.LastErrorSummary
		} else {
			id, err := newID()
			if err != nil {
				return Index{}, err
			}
			summary.ID = id
		}
		next = append(next, summary)
	}

	slices.SortFunc(next, func(a, b ConfigSummary) int {
		return compareStrings(a.SettingName, b.SettingName)
	})
	index.Configs = next
	if err := s.Save(index); err != nil {
		return Index{}, err
	}
	return index, nil
}

func (s *Store) RecordResult(id string, expiration *time.Time, errSummary string) (ConfigSummary, error) {
	if id == "" {
		return ConfigSummary{}, errors.New("id is required")
	}

	index, err := s.Load()
	if err != nil {
		return ConfigSummary{}, err
	}

	now := time.Now().UTC()
	for i, existing := range index.Configs {
		if existing.ID != id {
			continue
		}
		existing.LastRefreshTime = &now
		existing.LastKnownExpiration = expiration
		existing.LastErrorSummary = errSummary
		index.Configs[i] = existing
		if err := s.Save(index); err != nil {
			return ConfigSummary{}, err
		}
		return existing, nil
	}

	return ConfigSummary{}, fmt.Errorf("config not found: %s", id)
}

func (s *Store) ClearErrorSummaries() error {
	index, err := s.Load()
	if err != nil {
		return err
	}

	changed := false
	for i := range index.Configs {
		if index.Configs[i].LastErrorSummary == "" {
			continue
		}
		index.Configs[i].LastErrorSummary = ""
		changed = true
	}
	if !changed {
		return nil
	}
	return s.Save(index)
}

func validateInput(input ConfigInput) error {
	if input.SettingName == "" {
		return errors.New("settingName is required")
	}
	if input.OnePasswordAccountName == "" {
		return errors.New("onePasswordAccountName is required")
	}
	if input.ProfileName == "" {
		return errors.New("profileName is required")
	}
	if input.AuthType != "sts" && input.AuthType != "sso" {
		return errors.New("authType must be either 'sts' or 'sso'")
	}
	if input.AuthType == "sts" {
		if err := validateSTSInput(input); err != nil {
			return err
		}
	}
	return nil
}

func validateSTSInput(input ConfigInput) error {
	accessKeyID := strings.TrimSpace(input.AWSAccessKeyID)
	if accessKeyID != "" && !awsAccessKeyPattern.MatchString(accessKeyID) {
		return errors.New("awsAccessKeyId must be uppercase ASCII letters and digits only")
	}

	secretKey := input.AWSSecretAccessKey
	if secretKey != "" {
		if strings.TrimSpace(secretKey) != secretKey {
			return errors.New("awsSecretAccessKey must not contain leading or trailing whitespace")
		}
		if !utf8.ValidString(secretKey) {
			return errors.New("awsSecretAccessKey must be valid UTF-8")
		}
		for _, r := range secretKey {
			if r < 0x21 || r > 0x7e {
				return errors.New("awsSecretAccessKey must contain printable ASCII characters only")
			}
		}
	}

	roleArn := strings.TrimSpace(input.RoleArn)
	if roleArn != "" && !strings.HasPrefix(roleArn, "arn:") {
		return errors.New("roleArn must be blank or a valid AWS ARN")
	}

	return nil
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func itemKey(vaultID, itemID string) string {
	return vaultID + ":" + itemID
}
