package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yaman/aws-credential-manager/core-go/internal/generator"
	"github.com/yaman/aws-credential-manager/core-go/internal/ipc"
	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
	onepasswordmanager "github.com/yaman/aws-credential-manager/core-go/internal/onepassword"
	"github.com/yaman/aws-credential-manager/core-go/internal/settings"
)

type deleteConfigParams struct {
	ID string `json:"id"`
}

type getConfigParams struct {
	ID string `json:"id"`
}

type generateParams struct {
	ID string `json:"id"`
}

type onePasswordItemsParams struct {
	AccountName string `json:"accountName,omitempty"`
	VaultID     string `json:"vaultId"`
	ItemID      string `json:"itemId,omitempty"`
}

type onePasswordAccountParams struct {
	AccountName string `json:"accountName,omitempty"`
}

type Router struct {
	version       string
	store         *metadata.Store
	settingsStore *settings.Store
	opManager     *onepasswordmanager.Manager
	generator     *generator.Service
	mu            sync.Mutex
	generations   map[string]context.CancelFunc
}

func NewRouter(version string, store *metadata.Store, settingsStore *settings.Store, opManager *onepasswordmanager.Manager, generatorService *generator.Service) *Router {
	return &Router{
		version:       version,
		store:         store,
		settingsStore: settingsStore,
		opManager:     opManager,
		generator:     generatorService,
		generations:   map[string]context.CancelFunc{},
	}
}

func (r *Router) Handle(req ipc.Request) ipc.Response {
	switch req.Method {
	case "health.check":
		status := r.basicOnePasswordStatus()
		return ipc.Success(req.ID, map[string]any{
			"ok":               true,
			"version":          r.version,
			"pid":              getPID(),
			"time":             time.Now().UTC().Format(time.RFC3339),
			"onePassword":      status,
			"onePasswordReady": status.Connected,
		})
	case "configs.list":
		index, err := r.store.Ensure()
		if err != nil {
			return ipc.Failure(req.ID, "metadata_error", err.Error())
		}
		configs := r.enrichConfigSummaries(index.Configs)
		return ipc.Success(req.ID, map[string]any{
			"schemaVersion": index.SchemaVersion,
			"configs":       configs,
			"path":          r.store.Path(),
		})
	case "configs.sync":
		return r.handleSync(req)
	case "configs.get":
		return r.handleGet(req)
	case "configs.create":
		return r.handleCreate(req)
	case "configs.update":
		return r.handleUpdate(req)
	case "configs.delete":
		return r.handleDelete(req)
	case "configs.generate":
		return r.handleGenerate(req)
	case "configs.generate.cancel":
		return r.handleGenerateCancel(req)
	case "configs.errors.clear":
		return r.handleConfigErrorsClear(req)
	case "settings.get":
		return r.handleSettingsGet(req)
	case "settings.update":
		return r.handleSettingsUpdate(req)
	case "onepassword.status":
		return r.handleOnePasswordStatus(req)
	case "onepassword.reconnect":
		return r.handleOnePasswordReconnect(req)
	case "onepassword.vaults.list":
		return r.handleOnePasswordVaultsList(req)
	case "onepassword.items.list":
		return r.handleOnePasswordItemsList(req)
	case "onepassword.items.getConfig":
		return r.handleOnePasswordItemGetConfig(req)
	default:
		return ipc.Failure(req.ID, "method_not_found", fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (r *Router) basicOnePasswordStatus() onepasswordmanager.Status {
	settingsValue, err := r.settingsStore.Load()
	if err != nil {
		return onepasswordmanager.Status{
			Configured: false,
			Connected:  false,
			Message:    err.Error(),
		}
	}
	if settingsValue.SelectedOnePasswordAccountName == "" {
		return onepasswordmanager.Status{
			Configured: false,
			Connected:  false,
			Message:    "1Password account is not configured",
		}
	}
	return onepasswordmanager.Status{
		Configured:  true,
		Connected:   false,
		AccountName: settingsValue.SelectedOnePasswordAccountName,
		Message:     "1Password connection check pending",
	}
}

func (r *Router) handleGet(req ipc.Request) ipc.Response {
	var params getConfigParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	summary, err := r.store.Get(params.ID)
	if err != nil {
		return ipc.Failure(req.ID, "config_get_failed", err.Error())
	}

	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	input, err := r.opManager.LoadConfigItem(ctx, summary)
	if err != nil {
		return ipc.Failure(req.ID, "config_get_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"config": input,
	})
}

func (r *Router) handleCreate(req ipc.Request) ipc.Response {
	var input metadata.ConfigInput
	if err := json.Unmarshal(req.Params, &input); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	input.ID = ""

	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	inputWithItem, err := r.opManager.UpsertConfigItem(ctx, input)
	if err != nil {
		return ipc.Failure(req.ID, "config_create_failed", err.Error())
	}

	summary, err := r.store.Create(inputWithItem)
	if err != nil {
		return ipc.Failure(req.ID, "config_create_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"config": summary,
	})
}

func (r *Router) handleUpdate(req ipc.Request) ipc.Response {
	var input metadata.ConfigInput
	if err := json.Unmarshal(req.Params, &input); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	if input.ID == "" {
		return ipc.Failure(req.ID, "invalid_params", "id is required")
	}

	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	inputWithItem, err := r.opManager.UpsertConfigItem(ctx, input)
	if err != nil {
		return ipc.Failure(req.ID, "config_update_failed", err.Error())
	}

	summary, err := r.store.Update(inputWithItem)
	if err != nil {
		return ipc.Failure(req.ID, "config_update_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"config": summary,
	})
}

func (r *Router) handleDelete(req ipc.Request) ipc.Response {
	var params deleteConfigParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	if err := r.store.Delete(params.ID); err != nil {
		return ipc.Failure(req.ID, "config_delete_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"deletedID": params.ID,
	})
}

func (r *Router) handleConfigErrorsClear(req ipc.Request) ipc.Response {
	if err := r.store.ClearErrorSummaries(); err != nil {
		return ipc.Failure(req.ID, "config_errors_clear_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{})
}

func (r *Router) handleGenerate(req ipc.Request) ipc.Response {
	var params generateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	r.registerGeneration(params.ID, cancel)
	defer func() {
		r.unregisterGeneration(params.ID)
		cancel()
	}()
	result, err := r.generator.Generate(ctx, params.ID)
	if err != nil {
		return ipc.Failure(req.ID, "config_generate_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"result": result,
	})
}

func (r *Router) handleGenerateCancel(req ipc.Request) ipc.Response {
	var params generateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	if params.ID == "" {
		return ipc.Failure(req.ID, "invalid_params", "id is required")
	}
	cancelled := r.cancelGeneration(params.ID)
	return ipc.Success(req.ID, map[string]any{
		"cancelled": cancelled,
	})
}

func (r *Router) registerGeneration(id string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generations[id] = cancel
}

func (r *Router) unregisterGeneration(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.generations, id)
}

func (r *Router) cancelGeneration(id string) bool {
	r.mu.Lock()
	cancel, ok := r.generations[id]
	r.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

func (r *Router) handleSync(req ipc.Request) ipc.Response {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	summaries, err := r.opManager.ListManagedConfigSummaries(ctx)
	if err != nil {
		return ipc.Failure(req.ID, "config_sync_failed", err.Error())
	}
	index, err := r.store.SyncManagedSummaries(summaries)
	if err != nil {
		return ipc.Failure(req.ID, "config_sync_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"schemaVersion": index.SchemaVersion,
		"configs":       r.enrichConfigSummaries(index.Configs),
		"path":          r.store.Path(),
	})
}

func (r *Router) enrichConfigSummaries(configs []metadata.ConfigSummary) []metadata.ConfigSummary {
	enriched := make([]metadata.ConfigSummary, len(configs))
	copy(enriched, configs)
	selectedAccount := r.selectedOnePasswordAccountName()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for i := range enriched {
		if selectedAccount != "" {
			enriched[i].OnePasswordAccountName = selectedAccount
		}
		if enriched[i].AuthType != "sso" {
			continue
		}
		input, err := r.opManager.LoadConfigItem(ctx, enriched[i])
		if err != nil {
			continue
		}
		enriched[i].SSORefreshTokenAvailable = input.SSORefreshToken != ""
		if expiry, err := time.Parse(time.RFC3339, input.SSOAccessExpiry); err == nil {
			expiry = expiry.UTC()
			enriched[i].SSOSessionExpiry = &expiry
		}
	}
	return enriched
}

func (r *Router) selectedOnePasswordAccountName() string {
	if r.settingsStore == nil {
		return ""
	}
	settingsValue, err := r.settingsStore.Load()
	if err != nil {
		return ""
	}
	return settingsValue.SelectedOnePasswordAccountName
}

func (r *Router) handleSettingsGet(req ipc.Request) ipc.Response {
	settingsValue, err := r.settingsStore.Ensure()
	if err != nil {
		return ipc.Failure(req.ID, "settings_get_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"settings": settingsValue,
		"path":     r.settingsStore.Path(),
	})
}

func (r *Router) handleSettingsUpdate(req ipc.Request) ipc.Response {
	var params settings.AppSettings
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	params.SchemaVersion = settings.CurrentSchemaVersion
	if err := r.settingsStore.Save(params); err != nil {
		return ipc.Failure(req.ID, "settings_update_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{
		"settings": params,
		"path":     r.settingsStore.Path(),
	})
}

func (r *Router) handleOnePasswordStatus(req ipc.Request) ipc.Response {
	var params onePasswordAccountParams
	_ = json.Unmarshal(req.Params, &params)
	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	status := r.opManager.Status(ctx, params.AccountName)
	return ipc.Success(req.ID, map[string]any{
		"status": status,
	})
}

func (r *Router) handleOnePasswordReconnect(req ipc.Request) ipc.Response {
	var params onePasswordAccountParams
	_ = json.Unmarshal(req.Params, &params)
	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	status := r.opManager.Reconnect(ctx, params.AccountName)
	return ipc.Success(req.ID, map[string]any{
		"status": status,
	})
}

func (r *Router) handleOnePasswordVaultsList(req ipc.Request) ipc.Response {
	var params onePasswordAccountParams
	_ = json.Unmarshal(req.Params, &params)
	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	vaults, err := r.opManager.ListVaults(ctx, params.AccountName)
	if err != nil {
		return ipc.Failure(req.ID, "onepassword_vaults_list_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{"vaults": vaults})
}

func (r *Router) handleOnePasswordItemsList(req ipc.Request) ipc.Response {
	var params onePasswordItemsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	if params.VaultID == "" {
		return ipc.Failure(req.ID, "invalid_params", "vaultId is required")
	}
	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	items, err := r.opManager.ListManagedItems(ctx, params.AccountName, params.VaultID)
	if err != nil {
		return ipc.Failure(req.ID, "onepassword_items_list_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{"items": items})
}

func (r *Router) handleOnePasswordItemGetConfig(req ipc.Request) ipc.Response {
	var params onePasswordItemsParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ipc.Failure(req.ID, "invalid_params", err.Error())
	}
	if params.VaultID == "" || params.ItemID == "" {
		return ipc.Failure(req.ID, "invalid_params", "vaultId and itemId are required")
	}
	ctx, cancel := onepasswordmanager.WithInteractiveTimeout(context.Background())
	defer cancel()
	input, err := r.opManager.LoadConfigByItem(ctx, params.AccountName, params.VaultID, params.ItemID)
	if err != nil {
		return ipc.Failure(req.ID, "onepassword_item_get_config_failed", err.Error())
	}
	return ipc.Success(req.ID, map[string]any{"config": input})
}
