package onepasswordmanager

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	onepassword "github.com/1password/onepassword-sdk-go"

	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
	"github.com/yaman/aws-credential-manager/core-go/internal/sessioncache"
	"github.com/yaman/aws-credential-manager/core-go/internal/settings"
)

const (
	accountEnvVar      = "AWS_CREDENTIAL_MANAGER_1PASSWORD_ACCOUNT"
	integrationName    = "aws-credential-manager"
	integrationVer     = "0.1.5"
	managedTag         = "aws-credential-manager"
	schemaVersion      = "1"
	clientRetryMax     = 3
	clientMaxIdle      = 45 * time.Second
	requestTimeout     = 15 * time.Second
	interactiveTimeout = 3 * time.Minute

	commonSectionID = "aws-credential-manager"
	stsSectionID    = "sts-config"
	ssoSectionID    = "sso-config"
)

var preferredBuiltInVaultTitles = []string{
	"Private",
	"Personal",
	"Employee",
}

var rawTOTPSecretPattern = regexp.MustCompile(`^[A-Z2-7= ]{16,}$`)

type Status struct {
	Configured  bool   `json:"configured"`
	Connected   bool   `json:"connected"`
	AccountName string `json:"accountName,omitempty"`
	Message     string `json:"message"`
}

type Manager struct {
	mu              sync.Mutex
	sdkGate         chan struct{}
	sdkGateOnce     sync.Once
	client          *onepassword.Client
	account         string
	lastClientUse   time.Time
	settingsStore   *settings.Store
	clientFactory   func(ctx context.Context, accountName string) (*onepassword.Client, error)
	connectionProbe func(ctx context.Context, client *onepassword.Client) error
	vaultLister     func(ctx context.Context, client *onepassword.Client) ([]onepassword.VaultOverview, error)
	fieldResolver   func(ctx context.Context, client *onepassword.Client, vaultID, itemID, sectionID, fieldID, attribute string) (string, error)
}

type runtimeConfigLoader interface {
	LoadRuntimeConfig(ctx context.Context, summary metadata.ConfigSummary) (metadata.ConfigInput, error)
}

type managedSummaryLister interface {
	ListManagedConfigSummaries(ctx context.Context) ([]metadata.ConfigSummary, error)
}

type VaultOption struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ItemOption struct {
	VaultID     string `json:"vaultId"`
	ItemID      string `json:"itemId"`
	Title       string `json:"title"`
	SettingName string `json:"settingName"`
	AuthType    string `json:"authType"`
}

func NewManager(settingsStore *settings.Store) *Manager {
	return &Manager{settingsStore: settingsStore}
}

func (m *Manager) Reconnect(ctx context.Context, explicitAccountName string) Status {
	accountName, err := m.resolveAccountName(explicitAccountName)
	if err != nil {
		return Status{
			Configured: false,
			Connected:  false,
			Message:    err.Error(),
		}
	}

	m.resetClient(accountName)
	return m.Status(ctx, accountName)
}

func (m *Manager) Status(ctx context.Context, explicitAccountName string) Status {
	accountName, err := m.resolveAccountName(explicitAccountName)
	if err != nil {
		return Status{
			Configured: false,
			Connected:  false,
			Message:    err.Error(),
		}
	}

	if err := m.ensureConnected(ctx, accountName); err != nil {
		return Status{
			Configured:  true,
			Connected:   false,
			AccountName: accountName,
			Message:     err.Error(),
		}
	}

	return Status{
		Configured:  true,
		Connected:   true,
		AccountName: accountName,
		Message:     "Connected to 1Password desktop app",
	}
}

func (m *Manager) UpsertConfigItem(ctx context.Context, input metadata.ConfigInput) (metadata.ConfigInput, error) {
	accountName, err := m.resolveAccountName(input.OnePasswordAccountName)
	if err != nil {
		return input, err
	}
	input.OnePasswordAccountName = accountName

	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) (metadata.ConfigInput, error) {
		vaultID := strings.TrimSpace(input.VaultID)
		if vaultID == "" {
			vault, err := m.privateVault(ctx, client)
			if err != nil {
				return input, err
			}
			vaultID = vault.ID
		}

		title := itemTitle(input.SettingName)
		item, found, err := m.findExistingItem(ctx, client, vaultID, input.ItemID, title)
		if err != nil {
			return input, err
		}

		sections := buildManagedSections(input.AuthType)
		fields := buildManagedFields(input)
		tags := []string{managedTag}

		if found {
			item.Title = title
			item.Tags = ensureTags(item.Tags, tags)
			item.Category = onepassword.ItemCategoryLogin
			item.Sections = mergeManagedSections(item.Sections, sections)
			item.Fields = mergeManagedFields(item.Fields, fields)

			updated, err := client.Items().Put(ctx, item)
			if err != nil {
				return input, err
			}
			input.VaultID = updated.VaultID
			input.ItemID = updated.ID
			return input, nil
		}

		created, err := client.Items().Create(ctx, onepassword.ItemCreateParams{
			Title:    title,
			Category: onepassword.ItemCategoryLogin,
			VaultID:  vaultID,
			Sections: sections,
			Fields:   fields,
			Tags:     tags,
		})
		if err != nil {
			return input, err
		}

		input.VaultID = created.VaultID
		input.ItemID = created.ID
		return input, nil
	})
}

func (m *Manager) LoadConfigItem(ctx context.Context, summary metadata.ConfigSummary) (metadata.ConfigInput, error) {
	accountName, err := m.resolveAccountName(summary.OnePasswordAccountName)
	if err != nil {
		return metadata.ConfigInput{}, err
	}

	if summary.VaultID == "" || summary.ItemID == "" {
		return metadata.ConfigInput{}, errors.New("config is missing 1Password linkage")
	}
	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) (metadata.ConfigInput, error) {
		item, err := client.Items().Get(ctx, summary.VaultID, summary.ItemID)
		if err != nil {
			return metadata.ConfigInput{}, err
		}

		input := decodeConfigInput(item, summary.ID)
		input.OnePasswordAccountName = accountName
		if err := m.resolveStoredSecrets(ctx, client, item, &input); err != nil {
			return metadata.ConfigInput{}, err
		}
		return input, nil
	})
}

func (m *Manager) LoadRuntimeConfig(ctx context.Context, summary metadata.ConfigSummary) (metadata.ConfigInput, error) {
	accountName, err := m.resolveAccountName(summary.OnePasswordAccountName)
	if err != nil {
		return metadata.ConfigInput{}, err
	}

	if summary.VaultID == "" || summary.ItemID == "" {
		return metadata.ConfigInput{}, errors.New("config is missing 1Password linkage")
	}
	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) (metadata.ConfigInput, error) {
		item, err := client.Items().Get(ctx, summary.VaultID, summary.ItemID)
		if err != nil {
			return metadata.ConfigInput{}, err
		}

		input := decodeConfigInput(item, summary.ID)
		input.OnePasswordAccountName = accountName
		if err := m.resolveStoredSecrets(ctx, client, item, &input); err != nil {
			return metadata.ConfigInput{}, err
		}
		if err := m.resolveRuntimeSecrets(ctx, client, item, &input); err != nil {
			return metadata.ConfigInput{}, err
		}
		return input, nil
	})
}

func (m *Manager) LoadConfigByItem(ctx context.Context, accountName, vaultID, itemID string) (metadata.ConfigInput, error) {
	accountName, err := m.resolveAccountName(accountName)
	if err != nil {
		return metadata.ConfigInput{}, err
	}
	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) (metadata.ConfigInput, error) {
		item, err := client.Items().Get(ctx, vaultID, itemID)
		if err != nil {
			return metadata.ConfigInput{}, err
		}
		input := decodeConfigInput(item, "")
		input.OnePasswordAccountName = accountName
		if err := m.resolveStoredSecrets(ctx, client, item, &input); err != nil {
			return metadata.ConfigInput{}, err
		}
		return input, nil
	})
}

func (m *Manager) PersistSSOSessionState(ctx context.Context, summary metadata.ConfigSummary, session sessioncache.Session) error {
	accountName, err := m.resolveAccountName(summary.OnePasswordAccountName)
	if err != nil {
		return err
	}
	if summary.VaultID == "" || summary.ItemID == "" {
		return errors.New("config is missing 1Password linkage")
	}

	_, err = withClientRetry(ctx, m, accountName, func(client *onepassword.Client) (struct{}, error) {
		item, err := client.Items().Get(ctx, summary.VaultID, summary.ItemID)
		if err != nil {
			return struct{}{}, err
		}
		item.Fields = mergeSelectedManagedFields(item.Fields, buildSSOSessionStateFields(session), managedSSOSessionFieldIDs())
		_, err = client.Items().Put(ctx, item)
		return struct{}{}, err
	})
	return err
}

func (m *Manager) ListVaults(ctx context.Context, accountName string) ([]VaultOption, error) {
	accountName, err := m.resolveAccountName(accountName)
	if err != nil {
		return nil, err
	}
	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) ([]VaultOption, error) {
		vaults, err := m.listVaultOverviews(ctx, client)
		if err != nil {
			return nil, err
		}
		result := make([]VaultOption, 0, len(vaults))
		for _, vault := range vaults {
			result = append(result, VaultOption{ID: vault.ID, Title: vault.Title})
		}
		return result, nil
	})
}

func (m *Manager) ListManagedItems(ctx context.Context, accountName, vaultID string) ([]ItemOption, error) {
	accountName, err := m.resolveAccountName(accountName)
	if err != nil {
		return nil, err
	}
	m.resetClient(accountName)
	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) ([]ItemOption, error) {
		items, err := client.Items().List(ctx, vaultID, onepassword.NewItemListFilterTypeVariantByState(&onepassword.ItemListFilterByStateInner{
			Active:   true,
			Archived: false,
		}))
		if err != nil {
			return nil, err
		}
		result := make([]ItemOption, 0, len(items))
		for _, item := range items {
			settingName := strings.TrimSpace(item.Title)
			authType := ""
			if isManagedOverview(item) {
				fullItem, err := client.Items().Get(ctx, vaultID, item.ID)
				if err != nil {
					return nil, err
				}
				input := decodeConfigInput(fullItem, "")
				if strings.TrimSpace(input.SettingName) != "" {
					settingName = input.SettingName
				}
				authType = input.AuthType
			}
			result = append(result, ItemOption{
				VaultID:     vaultID,
				ItemID:      item.ID,
				Title:       item.Title,
				SettingName: settingName,
				AuthType:    authType,
			})
		}
		return result, nil
	})
}

func (m *Manager) ListManagedConfigSummaries(ctx context.Context) ([]metadata.ConfigSummary, error) {
	accountName, err := m.resolveAccountName("")
	if err != nil {
		return nil, err
	}

	return withClientRetry(ctx, m, accountName, func(client *onepassword.Client) ([]metadata.ConfigSummary, error) {
		vaults, err := client.Vaults().List(ctx)
		if err != nil {
			return nil, err
		}

		summaries := make([]metadata.ConfigSummary, 0, len(vaults))
		for _, vault := range vaults {
			items, err := client.Items().List(ctx, vault.ID, onepassword.NewItemListFilterTypeVariantByState(&onepassword.ItemListFilterByStateInner{
				Active:   true,
				Archived: false,
			}))
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if !isManagedOverview(item) {
					continue
				}
				fullItem, err := client.Items().Get(ctx, vault.ID, item.ID)
				if err != nil {
					return nil, err
				}
				input := decodeConfigInput(fullItem, "")
				if input.SettingName == "" || input.ProfileName == "" || input.AuthType == "" {
					continue
				}
				summaries = append(summaries, metadata.ConfigSummary{
					SettingName:            input.SettingName,
					AuthType:               input.AuthType,
					OnePasswordAccountName: accountName,
					ProfileName:            input.ProfileName,
					VaultID:                fullItem.VaultID,
					ItemID:                 fullItem.ID,
					AutoRefreshEnabled:     input.AutoRefreshEnabled,
				})
			}
		}
		return summaries, nil
	})
}

func (m *Manager) resolveAccountName(explicitAccountName string) (string, error) {
	if m.settingsStore != nil {
		settingsValue, err := m.settingsStore.Load()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(settingsValue.SelectedOnePasswordAccountName) != "" {
			return strings.TrimSpace(settingsValue.SelectedOnePasswordAccountName), nil
		}
	}
	if strings.TrimSpace(explicitAccountName) != "" {
		return strings.TrimSpace(explicitAccountName), nil
	}

	accountName := strings.TrimSpace(os.Getenv(accountEnvVar))
	if accountName == "" {
		return "", fmt.Errorf("1Password account is not configured")
	}
	return accountName, nil
}

func (m *Manager) clientForAccount(ctx context.Context, accountName string) (*onepassword.Client, error) {
	now := time.Now()

	m.mu.Lock()
	if m.client != nil && m.account == accountName {
		if !m.lastClientUse.IsZero() && now.Sub(m.lastClientUse) > clientMaxIdle {
			m.client = nil
			m.account = ""
			m.lastClientUse = time.Time{}
		} else {
			client := m.client
			m.lastClientUse = now
			m.mu.Unlock()
			return client, nil
		}
	}
	m.mu.Unlock()

	client, err := m.newClientWithRetry(ctx, accountName)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && m.account == accountName {
		m.lastClientUse = now
		return m.client, nil
	}
	m.client = client
	m.account = accountName
	m.lastClientUse = now
	return client, nil
}

func (m *Manager) newClient(ctx context.Context, accountName string) (*onepassword.Client, error) {
	if m.clientFactory != nil {
		return m.clientFactory(ctx, accountName)
	}
	return onepassword.NewClient(ctx,
		onepassword.WithDesktopAppIntegration(accountName),
		onepassword.WithIntegrationInfo(integrationName, integrationVer),
	)
}

func (m *Manager) resetClient(accountName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.account == accountName {
		m.client = nil
		m.account = ""
		m.lastClientUse = time.Time{}
	}
}

func (m *Manager) HasCachedClient(accountName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == nil || m.account != accountName {
		return false
	}
	if m.lastClientUse.IsZero() || time.Since(m.lastClientUse) > clientMaxIdle {
		return false
	}
	return true
}

func shouldRetryClientError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	retryableInitError := strings.Contains(message, "error initializing client:") &&
		(strings.Contains(message, "return code: -2") ||
			strings.Contains(message, "return code: -6") ||
			strings.Contains(message, "return code: -3") ||
			strings.Contains(message, "return code: -7"))
	switch {
	case retryableInitError:
		return true
	case strings.Contains(message, "desktop app connection channel is closed"):
		return true
	case strings.Contains(message, "connection channel is closed"):
		return true
	case strings.Contains(message, "connection was unexpectedly dropped by the desktop app"):
		return true
	case strings.Contains(message, "desktop application not found"):
		return true
	case strings.Contains(message, "context deadline exceeded"):
		return true
	case strings.Contains(message, "request timed out"):
		return true
	case strings.Contains(message, "timed out"):
		return true
	case strings.Contains(message, "broken pipe"):
		return true
	case strings.Contains(message, "connection reset by peer"):
		return true
	case strings.Contains(message, "eof"):
		return true
	default:
		return false
	}
}

func withClientRetry[T any](ctx context.Context, m *Manager, accountName string, operation func(*onepassword.Client) (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 0; attempt < clientRetryMax; attempt++ {
		client, err := m.clientForAccount(ctx, accountName)
		if err != nil {
			return zero, err
		}

		result, err := withSDKClient(ctx, m, client, operation)
		if err == nil || !shouldRetryClientError(err) || attempt == clientRetryMax-1 {
			return result, err
		}

		lastErr = err
		m.resetClient(accountName)
		if err := waitForRetry(ctx, attempt); err != nil {
			return zero, err
		}
	}

	return zero, lastErr
}

func withSDKClient[T any](ctx context.Context, m *Manager, client *onepassword.Client, operation func(*onepassword.Client) (T, error)) (T, error) {
	var zero T

	gate := m.sdkLockChannel()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-gate:
	}
	defer func() {
		gate <- struct{}{}
	}()

	return operation(client)
}

func (m *Manager) sdkLockChannel() chan struct{} {
	m.sdkGateOnce.Do(func() {
		m.sdkGate = make(chan struct{}, 1)
		m.sdkGate <- struct{}{}
	})
	return m.sdkGate
}

func (m *Manager) privateVault(ctx context.Context, client *onepassword.Client) (onepassword.VaultOverview, error) {
	vaults, err := m.listVaultOverviews(ctx, client)
	if err != nil {
		return onepassword.VaultOverview{}, err
	}

	for _, preferredTitle := range preferredBuiltInVaultTitles {
		for _, vault := range vaults {
			if strings.EqualFold(vault.Title, preferredTitle) {
				return vault, nil
			}
		}
	}

	for _, vault := range vaults {
		if strings.Contains(strings.ToLower(vault.Title), "private") || strings.Contains(strings.ToLower(vault.Title), "personal") || strings.Contains(strings.ToLower(vault.Title), "employee") {
			return vault, nil
		}
	}

	availableTitles := make([]string, 0, len(vaults))
	for _, vault := range vaults {
		availableTitles = append(availableTitles, vault.Title)
	}

	return onepassword.VaultOverview{}, fmt.Errorf(
		"no built-in personal vault found; expected one of %v, available vaults: %v",
		preferredBuiltInVaultTitles,
		availableTitles,
	)
}

func (m *Manager) ensureConnected(ctx context.Context, accountName string) error {
	_, err := withClientRetry(ctx, m, accountName, func(client *onepassword.Client) (struct{}, error) {
		return struct{}{}, m.probeConnection(ctx, client)
	})
	return err
}

func (m *Manager) probeConnection(ctx context.Context, client *onepassword.Client) error {
	if m.connectionProbe != nil {
		return m.connectionProbe(ctx, client)
	}
	_, err := m.listVaultOverviews(ctx, client)
	return err
}

func (m *Manager) listVaultOverviews(ctx context.Context, client *onepassword.Client) ([]onepassword.VaultOverview, error) {
	if m.vaultLister != nil {
		return m.vaultLister(ctx, client)
	}
	return client.Vaults().List(ctx)
}

func (m *Manager) newClientWithRetry(ctx context.Context, accountName string) (*onepassword.Client, error) {
	var lastErr error
	for attempt := 0; attempt < clientRetryMax; attempt++ {
		client, err := m.newClient(ctx, accountName)
		if err == nil {
			return client, nil
		}
		if !shouldRetryClientError(err) || attempt == clientRetryMax-1 {
			return nil, err
		}
		lastErr = err
		if err := waitForRetry(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func waitForRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt+1) * 200 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *Manager) findExistingItem(ctx context.Context, client *onepassword.Client, vaultID, itemID, title string) (onepassword.Item, bool, error) {
	if itemID != "" {
		item, err := client.Items().Get(ctx, vaultID, itemID)
		if err == nil {
			return item, true, nil
		}
	}

	items, err := client.Items().List(ctx, vaultID, onepassword.NewItemListFilterTypeVariantByState(&onepassword.ItemListFilterByStateInner{
		Active:   true,
		Archived: false,
	}))
	if err != nil {
		return onepassword.Item{}, false, err
	}

	for _, item := range items {
		if item.Title != title {
			continue
		}
		fullItem, err := client.Items().Get(ctx, vaultID, item.ID)
		if err != nil {
			return onepassword.Item{}, false, err
		}
		return fullItem, true, nil
	}

	return onepassword.Item{}, false, nil
}

func itemTitle(settingName string) string {
	return fmt.Sprintf("[aws-credential-manager] %s", settingName)
}

func isManagedOverview(item onepassword.ItemOverview) bool {
	if strings.HasPrefix(item.Title, "[aws-credential-manager] ") {
		return true
	}
	for _, tag := range item.Tags {
		if tag == managedTag {
			return true
		}
	}
	return false
}

func buildManagedSections(authType string) []onepassword.ItemSection {
	sections := []onepassword.ItemSection{
		{ID: commonSectionID, Title: "AWS Credential Manager"},
	}
	switch authType {
	case "sts":
		sections = append(sections, onepassword.ItemSection{ID: stsSectionID, Title: "STS Configuration"})
	case "sso":
		sections = append(sections, onepassword.ItemSection{ID: ssoSectionID, Title: "SSO Configuration"})
	}
	return sections
}

func buildManagedFields(input metadata.ConfigInput) []onepassword.ItemField {
	fields := []onepassword.ItemField{
		newField(commonSectionID, "setting_name", "Setting Name", onepassword.ItemFieldTypeText, input.SettingName),
		newField(commonSectionID, "account_name", "1Password Account Name", onepassword.ItemFieldTypeText, input.OnePasswordAccountName),
		newField(commonSectionID, "profile_name", "Profile Name", onepassword.ItemFieldTypeText, input.ProfileName),
		newField(commonSectionID, "auth_type", "Auth Type", onepassword.ItemFieldTypeText, input.AuthType),
		newField(commonSectionID, "auto_refresh_enabled", "Auto Refresh Enabled", onepassword.ItemFieldTypeText, fmt.Sprintf("%t", input.AutoRefreshEnabled)),
		newField(commonSectionID, "schema_version", "Schema Version", onepassword.ItemFieldTypeText, schemaVersion),
		newField(commonSectionID, "created_by", "Created By", onepassword.ItemFieldTypeText, integrationName),
	}

	switch input.AuthType {
	case "sts":
		fields = append(fields,
			newField(stsSectionID, "aws_access_key_id", "AWS Access Key ID", onepassword.ItemFieldTypeConcealed, input.AWSAccessKeyID),
			newField(stsSectionID, "aws_secret_access_key", "AWS Secret Access Key", onepassword.ItemFieldTypeConcealed, input.AWSSecretAccessKey),
			newField(stsSectionID, "mfa_arn", "MFA ARN", onepassword.ItemFieldTypeText, input.MFAArn),
			newMFAField(stsSectionID, canonicalTOTPFieldID("mfa_totp"), "MFA TOTP", input.MFATOTP),
			newField(stsSectionID, "role_arn", "Role ARN", onepassword.ItemFieldTypeText, input.RoleArn),
			newField(stsSectionID, "role_session_name", "Role Session Name", onepassword.ItemFieldTypeText, input.RoleSessionName),
			newField(stsSectionID, "external_id", "External ID", onepassword.ItemFieldTypeConcealed, input.ExternalID),
			newField(stsSectionID, "session_duration", "Session Duration Minutes", onepassword.ItemFieldTypeText, input.SessionDuration),
			newField(stsSectionID, "sts_region", "STS Region", onepassword.ItemFieldTypeText, input.STSRegion),
		)
	case "sso":
		fields = append(fields,
			newField(ssoSectionID, "sso_start_url", "SSO Start URL", onepassword.ItemFieldTypeURL, input.SSOStartURL),
			newField(ssoSectionID, "sso_issuer_url", "SSO Issuer URL", onepassword.ItemFieldTypeURL, input.SSOIssuerURL),
			newField(ssoSectionID, "sso_region", "SSO Region", onepassword.ItemFieldTypeText, input.SSORegion),
			newField(ssoSectionID, "sso_login_method", "SSO Sign-in Method", onepassword.ItemFieldTypeText, input.SSOLoginMethod),
			newField(ssoSectionID, "sso_username", "Username", onepassword.ItemFieldTypeText, input.SSOUsername),
			newField(ssoSectionID, "sso_password", "Password", onepassword.ItemFieldTypeConcealed, input.SSOPassword),
			newMFAField(ssoSectionID, canonicalTOTPFieldID("sso_mfa_totp"), "MFA TOTP", input.SSOMFATOTP),
			newField(ssoSectionID, "sso_account_id", "AWS Account ID", onepassword.ItemFieldTypeText, input.SSOAccountID),
			newField(ssoSectionID, "sso_role_name", "AWS Role Name", onepassword.ItemFieldTypeText, input.SSORoleName),
			newField(ssoSectionID, "session_duration", "Session Duration Minutes", onepassword.ItemFieldTypeText, input.SessionDuration),
			newField(ssoSectionID, "sso_access_token", "SSO Access Token", onepassword.ItemFieldTypeConcealed, input.SSOAccessToken),
			newField(ssoSectionID, "sso_access_expiry", "SSO Access Expiry", onepassword.ItemFieldTypeText, input.SSOAccessExpiry),
			newField(ssoSectionID, "sso_refresh_token", "SSO Refresh Token", onepassword.ItemFieldTypeConcealed, input.SSORefreshToken),
			newField(ssoSectionID, "sso_client_id", "SSO Client ID", onepassword.ItemFieldTypeText, input.SSOClientID),
			newField(ssoSectionID, "sso_client_secret", "SSO Client Secret", onepassword.ItemFieldTypeConcealed, input.SSOClientSecret),
			newField(ssoSectionID, "sso_client_secret_expiry", "SSO Client Secret Expiry", onepassword.ItemFieldTypeText, input.SSOClientSecretExpiry),
			newField(ssoSectionID, "sso_last_browser_url", "SSO Last Browser URL", onepassword.ItemFieldTypeURL, input.SSOLastBrowserURL),
		)
	}

	return pruneEmptyFields(fields)
}

func buildSSOSessionStateFields(session sessioncache.Session) []onepassword.ItemField {
	return pruneEmptyFields([]onepassword.ItemField{
		newField(ssoSectionID, "sso_access_token", "SSO Access Token", onepassword.ItemFieldTypeConcealed, session.AccessToken),
		newField(ssoSectionID, "sso_access_expiry", "SSO Access Expiry", onepassword.ItemFieldTypeText, formatSessionTime(session.AccessExpiry)),
		newField(ssoSectionID, "sso_refresh_token", "SSO Refresh Token", onepassword.ItemFieldTypeConcealed, session.RefreshToken),
		newField(ssoSectionID, "sso_client_id", "SSO Client ID", onepassword.ItemFieldTypeText, session.Registration.ClientID),
		newField(ssoSectionID, "sso_client_secret", "SSO Client Secret", onepassword.ItemFieldTypeConcealed, session.Registration.ClientSecret),
		newField(ssoSectionID, "sso_client_secret_expiry", "SSO Client Secret Expiry", onepassword.ItemFieldTypeText, formatSessionTime(session.Registration.ClientSecretExpiresAt)),
		newField(ssoSectionID, "sso_last_browser_url", "SSO Last Browser URL", onepassword.ItemFieldTypeURL, session.LastBrowserURL),
	})
}

func managedSSOSessionFieldIDs() map[string]bool {
	return map[string]bool{
		"sso_access_token":         true,
		"sso_access_expiry":        true,
		"sso_refresh_token":        true,
		"sso_client_id":            true,
		"sso_client_secret":        true,
		"sso_client_secret_expiry": true,
		"sso_last_browser_url":     true,
	}
}

func formatSessionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func decodeConfigInput(item onepassword.Item, id string) metadata.ConfigInput {
	input := metadata.ConfigInput{
		ID:      id,
		VaultID: item.VaultID,
		ItemID:  item.ID,
	}

	for _, field := range item.Fields {
		value := field.Value
		switch canonicalManagedFieldID(field) {
		case "setting_name":
			input.SettingName = value
		case "account_name":
			input.OnePasswordAccountName = value
		case "profile_name":
			input.ProfileName = value
		case "auth_type":
			input.AuthType = value
		case "auto_refresh_enabled":
			input.AutoRefreshEnabled = strings.EqualFold(value, "true")
		case "aws_access_key_id":
			input.AWSAccessKeyID = value
		case "aws_secret_access_key":
			input.AWSSecretAccessKey = value
		case "mfa_arn":
			input.MFAArn = value
		case "mfa_totp":
			input.MFATOTP = value
		case "role_arn":
			input.RoleArn = value
		case "role_session_name":
			input.RoleSessionName = value
		case "external_id":
			input.ExternalID = value
		case "session_duration":
			input.SessionDuration = value
		case "sts_region":
			input.STSRegion = value
		case "sso_start_url":
			input.SSOStartURL = value
		case "sso_issuer_url":
			input.SSOIssuerURL = value
		case "sso_region":
			input.SSORegion = value
		case "sso_login_method":
			input.SSOLoginMethod = value
		case "sso_username":
			input.SSOUsername = value
		case "sso_password":
			input.SSOPassword = value
		case "sso_mfa_totp":
			input.SSOMFATOTP = value
		case "sso_account_id":
			input.SSOAccountID = value
		case "sso_role_name":
			input.SSORoleName = value
		case "sso_access_token":
			input.SSOAccessToken = value
		case "sso_access_expiry":
			input.SSOAccessExpiry = value
		case "sso_refresh_token":
			input.SSORefreshToken = value
		case "sso_client_id":
			input.SSOClientID = value
		case "sso_client_secret":
			input.SSOClientSecret = value
		case "sso_client_secret_expiry":
			input.SSOClientSecretExpiry = value
		case "sso_last_browser_url":
			input.SSOLastBrowserURL = value
		}
	}

	applyImportFallbacks(item, &input)
	return input
}

func applyImportFallbacks(item onepassword.Item, input *metadata.ConfigInput) {
	for _, field := range item.Fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			continue
		}
		switch canonicalImportFieldID(field) {
		case "setting_name":
			if strings.TrimSpace(input.SettingName) == "" {
				input.SettingName = value
			}
		case "account_name":
			if strings.TrimSpace(input.OnePasswordAccountName) == "" {
				input.OnePasswordAccountName = value
			}
		case "profile_name":
			if strings.TrimSpace(input.ProfileName) == "" {
				input.ProfileName = value
			}
		case "auth_type":
			if strings.TrimSpace(input.AuthType) == "" {
				input.AuthType = strings.ToLower(value)
			}
		case "aws_access_key_id":
			if strings.TrimSpace(input.AWSAccessKeyID) == "" {
				input.AWSAccessKeyID = value
			}
		case "aws_secret_access_key":
			if strings.TrimSpace(input.AWSSecretAccessKey) == "" {
				input.AWSSecretAccessKey = value
			}
		case "mfa_arn":
			if strings.TrimSpace(input.MFAArn) == "" {
				input.MFAArn = value
			}
		case "mfa_totp":
			if strings.TrimSpace(input.MFATOTP) == "" {
				input.MFATOTP = value
			}
		case "role_arn":
			if strings.TrimSpace(input.RoleArn) == "" {
				input.RoleArn = value
			}
		case "role_session_name":
			if strings.TrimSpace(input.RoleSessionName) == "" {
				input.RoleSessionName = value
			}
		case "external_id":
			if strings.TrimSpace(input.ExternalID) == "" {
				input.ExternalID = value
			}
		case "session_duration":
			if strings.TrimSpace(input.SessionDuration) == "" {
				input.SessionDuration = value
			}
		case "sts_region":
			if strings.TrimSpace(input.STSRegion) == "" {
				input.STSRegion = value
			}
		case "sso_start_url":
			if strings.TrimSpace(input.SSOStartURL) == "" {
				input.SSOStartURL = value
			}
		case "sso_issuer_url":
			if strings.TrimSpace(input.SSOIssuerURL) == "" {
				input.SSOIssuerURL = value
			}
		case "sso_region":
			if strings.TrimSpace(input.SSORegion) == "" {
				input.SSORegion = value
			}
		case "sso_login_method":
			if strings.TrimSpace(input.SSOLoginMethod) == "" {
				input.SSOLoginMethod = value
			}
		case "sso_username":
			if strings.TrimSpace(input.SSOUsername) == "" {
				input.SSOUsername = value
			}
		case "sso_password":
			if strings.TrimSpace(input.SSOPassword) == "" {
				input.SSOPassword = value
			}
		case "sso_mfa_totp":
			if strings.TrimSpace(input.SSOMFATOTP) == "" {
				input.SSOMFATOTP = value
			}
		case "sso_account_id":
			if strings.TrimSpace(input.SSOAccountID) == "" {
				input.SSOAccountID = value
			}
		case "sso_role_name":
			if strings.TrimSpace(input.SSORoleName) == "" {
				input.SSORoleName = value
			}
		case "sso_access_token":
			if strings.TrimSpace(input.SSOAccessToken) == "" {
				input.SSOAccessToken = value
			}
		case "sso_access_expiry":
			if strings.TrimSpace(input.SSOAccessExpiry) == "" {
				input.SSOAccessExpiry = value
			}
		case "sso_refresh_token":
			if strings.TrimSpace(input.SSORefreshToken) == "" {
				input.SSORefreshToken = value
			}
		case "sso_client_id":
			if strings.TrimSpace(input.SSOClientID) == "" {
				input.SSOClientID = value
			}
		case "sso_client_secret":
			if strings.TrimSpace(input.SSOClientSecret) == "" {
				input.SSOClientSecret = value
			}
		case "sso_client_secret_expiry":
			if strings.TrimSpace(input.SSOClientSecretExpiry) == "" {
				input.SSOClientSecretExpiry = value
			}
		case "sso_last_browser_url":
			if strings.TrimSpace(input.SSOLastBrowserURL) == "" {
				input.SSOLastBrowserURL = value
			}
		}
	}

	if strings.TrimSpace(input.SettingName) == "" {
		input.SettingName = strings.TrimSpace(item.Title)
	}
	if strings.TrimSpace(input.AuthType) == "" {
		switch {
		case strings.TrimSpace(input.SSOStartURL) != "" || strings.TrimSpace(input.SSOAccountID) != "" || strings.TrimSpace(input.SSORoleName) != "":
			input.AuthType = "sso"
		case strings.TrimSpace(input.AWSAccessKeyID) != "" || strings.TrimSpace(input.MFAArn) != "" || strings.TrimSpace(input.RoleArn) != "":
			input.AuthType = "sts"
		}
	}
	if strings.TrimSpace(input.AuthType) == "sso" && strings.TrimSpace(input.SSOLoginMethod) == "" {
		input.SSOLoginMethod = "deviceCode"
	}
}

func (m *Manager) resolveStoredSecrets(ctx context.Context, client *onepassword.Client, item onepassword.Item, input *metadata.ConfigInput) error {
	for _, field := range item.Fields {
		canonicalID := canonicalManagedFieldID(field)
		if !isManagedSecretField(canonicalID) {
			continue
		}
		if hasFieldValue(*input, canonicalID) {
			continue
		}
		if field.FieldType == onepassword.ItemFieldTypeTOTP {
			// Keep the stored otpauth URL from Item.Get for edit screens.
			continue
		}
		if field.SectionID == nil {
			continue
		}
		value, err := m.resolveFieldValue(ctx, client, item.VaultID, item.ID, *field.SectionID, field.ID, "value")
		if err != nil {
			return err
		}
		assignFieldValue(input, canonicalID, value)
	}
	return nil
}

func (m *Manager) resolveRuntimeSecrets(ctx context.Context, client *onepassword.Client, item onepassword.Item, input *metadata.ConfigInput) error {
	for _, field := range item.Fields {
		if field.SectionID == nil {
			continue
		}
		switch canonicalManagedFieldID(field) {
		case "mfa_totp", "sso_mfa_totp":
			if field.FieldType == onepassword.ItemFieldTypeTOTP && field.Details != nil {
				if otp := field.Details.OTP(); otp != nil && otp.Code != nil && strings.TrimSpace(*otp.Code) != "" {
					assignFieldValue(input, canonicalManagedFieldID(field), strings.TrimSpace(*otp.Code))
					continue
				}
			}
			attribute := "value"
			if field.FieldType == onepassword.ItemFieldTypeTOTP {
				attribute = "totp"
			}
			value, err := m.resolveFieldValue(ctx, client, item.VaultID, item.ID, *field.SectionID, field.ID, attribute)
			if err != nil {
				return err
			}
			assignFieldValue(input, canonicalManagedFieldID(field), value)
		}
	}
	return nil
}

func resolveField(ctx context.Context, client *onepassword.Client, vaultID, itemID, sectionID, fieldID, attribute string) (string, error) {
	ref := fmt.Sprintf("op://%s/%s/%s/%s?attribute=%s", vaultID, itemID, sectionID, fieldID, attribute)
	return client.Secrets().Resolve(ctx, ref)
}

func (m *Manager) resolveFieldValue(ctx context.Context, client *onepassword.Client, vaultID, itemID, sectionID, fieldID, attribute string) (string, error) {
	if m.fieldResolver != nil {
		return m.fieldResolver(ctx, client, vaultID, itemID, sectionID, fieldID, attribute)
	}
	return resolveField(ctx, client, vaultID, itemID, sectionID, fieldID, attribute)
}

func isManagedSecretField(id string) bool {
	switch id {
	case "aws_access_key_id", "aws_secret_access_key", "mfa_totp", "external_id", "sso_password", "sso_mfa_totp", "sso_access_token", "sso_refresh_token", "sso_client_secret":
		return true
	default:
		return false
	}
}

func assignFieldValue(input *metadata.ConfigInput, fieldID, value string) {
	switch fieldID {
	case "aws_access_key_id":
		input.AWSAccessKeyID = value
	case "aws_secret_access_key":
		input.AWSSecretAccessKey = value
	case "mfa_totp":
		input.MFATOTP = value
	case "external_id":
		input.ExternalID = value
	case "sso_password":
		input.SSOPassword = value
	case "sso_mfa_totp":
		input.SSOMFATOTP = value
	case "sso_access_token":
		input.SSOAccessToken = value
	case "sso_access_expiry":
		input.SSOAccessExpiry = value
	case "sso_refresh_token":
		input.SSORefreshToken = value
	case "sso_client_id":
		input.SSOClientID = value
	case "sso_client_secret":
		input.SSOClientSecret = value
	case "sso_client_secret_expiry":
		input.SSOClientSecretExpiry = value
	case "sso_last_browser_url":
		input.SSOLastBrowserURL = value
	}
}

func hasFieldValue(input metadata.ConfigInput, fieldID string) bool {
	switch fieldID {
	case "aws_access_key_id":
		return strings.TrimSpace(input.AWSAccessKeyID) != ""
	case "aws_secret_access_key":
		return strings.TrimSpace(input.AWSSecretAccessKey) != ""
	case "mfa_totp":
		return strings.TrimSpace(input.MFATOTP) != ""
	case "external_id":
		return strings.TrimSpace(input.ExternalID) != ""
	case "sso_password":
		return strings.TrimSpace(input.SSOPassword) != ""
	case "sso_mfa_totp":
		return strings.TrimSpace(input.SSOMFATOTP) != ""
	case "sso_access_token":
		return strings.TrimSpace(input.SSOAccessToken) != ""
	case "sso_access_expiry":
		return strings.TrimSpace(input.SSOAccessExpiry) != ""
	case "sso_refresh_token":
		return strings.TrimSpace(input.SSORefreshToken) != ""
	case "sso_client_id":
		return strings.TrimSpace(input.SSOClientID) != ""
	case "sso_client_secret":
		return strings.TrimSpace(input.SSOClientSecret) != ""
	case "sso_client_secret_expiry":
		return strings.TrimSpace(input.SSOClientSecretExpiry) != ""
	case "sso_last_browser_url":
		return strings.TrimSpace(input.SSOLastBrowserURL) != ""
	default:
		return false
	}
}

func pruneEmptyFields(fields []onepassword.ItemField) []onepassword.ItemField {
	result := make([]onepassword.ItemField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		result = append(result, field)
	}
	return result
}

func mergeManagedFields(existing []onepassword.ItemField, replacement []onepassword.ItemField) []onepassword.ItemField {
	managedSectionIDs := map[string]bool{
		commonSectionID: true,
		stsSectionID:    true,
		ssoSectionID:    true,
	}
	activeSections := map[string]bool{}
	for _, field := range replacement {
		if field.SectionID != nil {
			activeSections[*field.SectionID] = true
		}
	}
	replacementByID := make(map[string]onepassword.ItemField, len(replacement))
	for _, field := range replacement {
		replacementByID[canonicalManagedFieldID(field)] = field
	}

	merged := make([]onepassword.ItemField, 0, len(existing)+len(replacement))
	used := map[string]bool{}
	for _, field := range existing {
		if field.SectionID != nil && managedSectionIDs[*field.SectionID] {
			canonicalID := canonicalManagedFieldID(field)
			if used[canonicalID] {
				continue
			}
			if next, ok := replacementByID[canonicalID]; ok {
				merged = append(merged, next)
				used[canonicalID] = true
			} else if activeSections[*field.SectionID] && shouldPreserveManagedField(canonicalID) {
				merged = append(merged, field)
				used[canonicalID] = true
			}
			continue
		}
		merged = append(merged, field)
	}

	for _, field := range replacement {
		canonicalID := canonicalManagedFieldID(field)
		if used[canonicalID] {
			continue
		}
		merged = append(merged, field)
		used[canonicalID] = true
	}
	return merged
}

func mergeSelectedManagedFields(existing []onepassword.ItemField, replacement []onepassword.ItemField, selectedIDs map[string]bool) []onepassword.ItemField {
	replacementByID := make(map[string]onepassword.ItemField, len(replacement))
	for _, field := range replacement {
		replacementByID[canonicalManagedFieldID(field)] = field
	}

	merged := make([]onepassword.ItemField, 0, len(existing)+len(replacement))
	used := map[string]bool{}
	for _, field := range existing {
		canonicalID := canonicalManagedFieldID(field)
		if selectedIDs[canonicalID] {
			if next, ok := replacementByID[canonicalID]; ok {
				merged = append(merged, next)
				used[canonicalID] = true
			}
			continue
		}
		merged = append(merged, field)
	}

	for _, field := range replacement {
		canonicalID := canonicalManagedFieldID(field)
		if !selectedIDs[canonicalID] || used[canonicalID] {
			continue
		}
		merged = append(merged, field)
		used[canonicalID] = true
	}
	return merged
}

func shouldPreserveManagedField(id string) bool {
	if isManagedSecretField(id) {
		return true
	}
	return managedSSOSessionFieldIDs()[id]
}

func mergeManagedSections(existing []onepassword.ItemSection, replacement []onepassword.ItemSection) []onepassword.ItemSection {
	merged := make([]onepassword.ItemSection, 0, len(existing)+len(replacement))
	replacementByID := make(map[string]onepassword.ItemSection, len(replacement))
	for _, section := range replacement {
		replacementByID[section.ID] = section
	}
	used := map[string]bool{}

	for _, section := range existing {
		if next, ok := replacementByID[section.ID]; ok {
			merged = append(merged, next)
			used[section.ID] = true
			continue
		}
		if section.ID == commonSectionID || section.ID == stsSectionID || section.ID == ssoSectionID {
			continue
		}
		merged = append(merged, section)
	}

	for _, section := range replacement {
		if used[section.ID] {
			continue
		}
		merged = append(merged, section)
	}

	return merged
}

func newField(sectionID, id, title string, fieldType onepassword.ItemFieldType, value string) onepassword.ItemField {
	section := sectionID
	return onepassword.ItemField{
		ID:        id,
		Title:     title,
		SectionID: &section,
		FieldType: fieldType,
		Value:     value,
	}
}

func newMFAField(sectionID, id, title, value string) onepassword.ItemField {
	normalizedValue := normalizeTOTPValue(id, value)
	fieldType := onepassword.ItemFieldTypeConcealed
	if strings.HasPrefix(strings.TrimSpace(normalizedValue), "otpauth://") {
		fieldType = onepassword.ItemFieldTypeTOTP
	}
	return newField(sectionID, id, title, fieldType, normalizedValue)
}

func canonicalManagedFieldID(field onepassword.ItemField) string {
	id := field.ID
	if field.FieldType == onepassword.ItemFieldTypeTOTP {
		id = strings.TrimPrefix(id, "TOTP_")
	}
	return id
}

func canonicalImportFieldID(field onepassword.ItemField) string {
	if managedID := canonicalManagedFieldID(field); managedID != "" && managedID != field.ID {
		return managedID
	}

	candidates := []string{field.ID, field.Title}
	for _, candidate := range candidates {
		switch normalizeImportKey(candidate) {
		case "settingname":
			return "setting_name"
		case "profilename":
			return "profile_name"
		case "authtype":
			return "auth_type"
		case "awsaccesskeyid", "accesskeyid":
			return "aws_access_key_id"
		case "awssecretaccesskey", "secretaccesskey":
			return "aws_secret_access_key"
		case "mfaarn":
			return "mfa_arn"
		case "mfatotp", "totp", "otp":
			return "mfa_totp"
		case "rolearn":
			return "role_arn"
		case "rolesessionname":
			return "role_session_name"
		case "externalid":
			return "external_id"
		case "sessionduration", "sessiondurationminutes":
			return "session_duration"
		case "stsregion", "region":
			return "sts_region"
		case "ssostarturl", "starturl":
			return "sso_start_url"
		case "ssoregion":
			return "sso_region"
		case "ssousername", "username":
			return "sso_username"
		case "ssopassword", "password":
			return "sso_password"
		case "ssomfatotp":
			return "sso_mfa_totp"
		case "awsaccountid", "accountid", "ssoaccountid":
			return "sso_account_id"
		case "awsrolename", "rolename", "ssorolename":
			return "sso_role_name"
		}
	}
	return ""
}

func normalizeImportKey(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func canonicalTOTPFieldID(id string) string {
	return "TOTP_" + strings.TrimPrefix(id, "TOTP_")
}

func normalizeTOTPValue(fieldID, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "otpauth://") {
		return trimmed
	}
	secret := strings.ToUpper(strings.ReplaceAll(trimmed, " ", ""))
	if !rawTOTPSecretPattern.MatchString(secret) {
		return value
	}

	label := url.QueryEscape(fmt.Sprintf("%s:%s", integrationName, fieldID))
	issuer := url.QueryEscape(integrationName)
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s", label, secret, issuer)
}

func ensureTags(existing []string, additions []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(existing)+len(additions))
	for _, tag := range existing {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
	}
	for _, tag := range additions {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
	}
	return result
}

func WithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, requestTimeout)
}

func WithInteractiveTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, interactiveTimeout)
}
