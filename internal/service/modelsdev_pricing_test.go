package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/awsl-project/maxx/internal/domain"
	"github.com/awsl-project/maxx/internal/pricing"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
)

func TestUpdateModelPricesFromModelsDevReplacesCurrentPrices(t *testing.T) {
	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "maxx-pricing-updater") {
			t.Fatalf("User-Agent = %q, want maxx pricing updater", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"openai": {
				"models": {
					"gpt-remote": {
						"id": "gpt-remote",
						"cost": {"input": 1.25, "output": 10, "cache_read": 0.125}
					},
					"gpt-unavailable": {
						"id": "gpt-unavailable",
						"cost": {"input": 99, "output": 100}
					}
				}
			}
		}`)
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo, "gpt-remote")
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID)
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("updated prices count = %d, want 1", len(prices))
	}
	if repo.hasModel("old-model") {
		t.Fatal("old-model should have been wiped before models.dev prices were inserted")
	}
	if repo.hasModel("gpt-unavailable") {
		t.Fatal("gpt-unavailable should have been skipped because it is not in the available model list")
	}

	remote := repo.mustModel(t, "gpt-remote")
	if remote.InputPriceMicro != 1_250_000 || remote.OutputPriceMicro != 10_000_000 {
		t.Fatalf("remote prices = %d/%d, want 1250000/10000000", remote.InputPriceMicro, remote.OutputPriceMicro)
	}
	if remote.CacheReadPriceMicro != 125_000 {
		t.Fatalf("remote cache read = %d, want 125000", remote.CacheReadPriceMicro)
	}
}

func TestUpdateModelPricesFromModelsDevFallsBackToBuiltInDefaults(t *testing.T) {
	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo, "claude-sonnet-4-5")
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID)
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() fallback error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("fallback prices count = %d, want 1 accessible default", len(prices))
	}
	if repo.hasModel("old-model") {
		t.Fatal("old-model should have been wiped before fallback defaults were inserted")
	}
	if !repo.hasModel("claude-sonnet-4-5") {
		t.Fatal("expected hardcoded default price claude-sonnet-4-5 after fallback")
	}
	if repo.hasModel("gpt-5.4") {
		t.Fatal("did not expect inaccessible built-in default price gpt-5.4 after fallback")
	}
}

func TestResetModelPricesToDefaultsWipesModelsDevPrices(t *testing.T) {
	repo := newAdminServiceModelPriceRepo(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"openai": {
				"models": {
					"gpt-remote-only": {
						"id": "gpt-remote-only",
						"cost": {"input": 1, "output": 2}
					}
				}
			}
		}`)
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo, "gpt-remote-only")
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	if _, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID); err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if !repo.hasModel("gpt-remote-only") {
		t.Fatal("expected models.dev-only price after update")
	}

	resetPrices, err := svc.ResetModelPricesToDefaults()
	if err != nil {
		t.Fatalf("ResetModelPricesToDefaults() error = %v", err)
	}
	if len(resetPrices) != len(pricing.DefaultPriceTable().All()) {
		t.Fatalf("reset prices count = %d, want %d", len(resetPrices), len(pricing.DefaultPriceTable().All()))
	}
	if repo.hasModel("gpt-remote-only") {
		t.Fatal("models.dev-only price should be wiped by reset")
	}
	if !repo.hasModel("claude-sonnet-4-5") {
		t.Fatal("expected hardcoded default price claude-sonnet-4-5 after reset")
	}
}

func TestUpdateModelPricesFromModelsDevRequiresAvailableModels(t *testing.T) {
	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	svc := newModelPriceOnlyAdminService(repo)

	_, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID)
	if err == nil {
		t.Fatal("expected update to fail when no accessible models are available")
	}
	if !repo.hasModel("old-model") {
		t.Fatal("old prices should be preserved when update is skipped")
	}
}

func TestUpdateModelPricesFromModelsDevUsesProviderModelEndpointAvailability(t *testing.T) {
	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-provider" {
			t.Fatalf("Authorization = %q, want provider bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-provider-endpoint"}]}`))
	}))
	defer modelsServer.Close()

	pricingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openai": {
				"models": {
					"gpt-provider-endpoint": {
						"id": "gpt-provider-endpoint",
						"cost": {"input": 1, "output": 2}
					},
					"gpt-not-advertised": {
						"id": "gpt-not-advertised",
						"cost": {"input": 3, "output": 4}
					}
				}
			}
		}`))
	}))
	defer pricingServer.Close()

	svc := newModelPriceOnlyAdminService(repo)
	svc.providerRepo = &adminServiceProviderRepo{providers: []*domain.Provider{{
		TenantID: domain.DefaultTenantID,
		Type:     "custom",
		Name:     "cliproxy-provider",
		Config: &domain.ProviderConfig{Custom: &domain.ProviderConfigCustom{
			BaseURL: modelsServer.URL,
			APIKey:  "sk-provider",
		}},
	}}}
	svc.SetModelsDevPricingSource(pricingServer.URL, pricingServer.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID)
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("updated price count = %d, want 1", len(prices))
	}
	if !repo.hasModel("gpt-provider-endpoint") {
		t.Fatal("expected provider models endpoint availability to feed pricing update")
	}
	if repo.hasModel("gpt-not-advertised") {
		t.Fatal("did not expect non-advertised model to be imported")
	}
}

func TestUpdateModelPricesFromModelsDevUsesCLIProxyRegistryAvailability(t *testing.T) {
	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openai": {
				"models": {
					"gpt-registry-model": {
						"id": "gpt-registry-model",
						"cost": {"input": 1, "output": 2}
					},
					"gpt-not-advertised": {
						"id": "gpt-not-advertised",
						"cost": {"input": 3, "output": 4}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo)
	svc.SetModelAvailabilityRegistry(fakeModelAvailabilityRegistry{
		handlerModels: map[string][]map[string]any{
			"openai": {{"id": "gpt-registry-model"}},
		},
	})
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID)
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("updated price count = %d, want 1", len(prices))
	}
	if !repo.hasModel("gpt-registry-model") {
		t.Fatal("expected CLIProxyAPI registry availability to feed pricing update")
	}
	if repo.hasModel("gpt-not-advertised") {
		t.Fatal("did not expect non-advertised model to be imported")
	}
}

func TestUpdateModelPricesFromModelsDevUsesGlobalCLIProxyRegistryAvailability(t *testing.T) {
	const clientID = "admin-service-global-registry-test"
	registry := cliproxy.GlobalModelRegistry()
	registry.RegisterClient(clientID, "codex", []*cliproxy.ModelInfo{{ID: "gpt-global-registry-model"}})
	t.Cleanup(func() { registry.UnregisterClient(clientID) })

	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openai": {
				"models": {
					"gpt-global-registry-model": {
						"id": "gpt-global-registry-model",
						"cost": {"input": 1, "output": 2}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo)
	svc.modelRegistry = nil
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.DefaultTenantID)
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("updated price count = %d, want 1", len(prices))
	}
	if !repo.hasModel("gpt-global-registry-model") {
		t.Fatal("expected global CLIProxyAPI registry availability to feed pricing update")
	}
}

func TestUpdateModelPricesFromModelsDevKeepsTenantAllProviderAvailability(t *testing.T) {
	repo := newAdminServiceModelPriceRepo([]*domain.ModelPrice{{
		ModelID:          "old-model",
		InputPriceMicro:  1,
		OutputPriceMicro: 2,
	}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openai": {
				"models": {
					"gpt-tenant-all-mapping": {
						"id": "gpt-tenant-all-mapping",
						"cost": {"input": 1, "output": 2}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo)
	svc.providerRepo = &adminServiceProviderRepo{providers: []*domain.Provider{{
		TenantID: domain.TenantIDAll,
		Type:     "custom",
		Name:     "global-provider",
		SupportModels: []string{
			"gpt-tenant-all-mapping",
		},
	}}}
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background(), domain.TenantIDAll)
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("updated price count = %d, want 1", len(prices))
	}
	if !repo.hasModel("gpt-tenant-all-mapping") {
		t.Fatal("expected tenant-all model availability to feed pricing update")
	}
}

func newModelPriceOnlyAdminService(repo *adminServiceModelPriceRepo, supportModels ...string) *AdminService {
	providerRepo := &adminServiceProviderRepo{}
	if len(supportModels) > 0 {
		providerRepo.providers = []*domain.Provider{{
			TenantID:      domain.DefaultTenantID,
			Type:          "custom",
			Name:          "available-models",
			SupportModels: append([]string(nil), supportModels...),
		}}
	}
	svc := NewAdminService(
		providerRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		repo,
		"",
		nil,
		nil,
		nil,
	)
	svc.SetModelAvailabilityRegistry(fakeModelAvailabilityRegistry{})
	return svc
}

type fakeModelAvailabilityRegistry struct {
	handlerModels  map[string][]map[string]any
	providerModels map[string][]*cliproxy.ModelInfo
}

func (r fakeModelAvailabilityRegistry) GetAvailableModels(handlerType string) []map[string]any {
	return r.handlerModels[handlerType]
}

func (r fakeModelAvailabilityRegistry) GetAvailableModelsByProvider(provider string) []*cliproxy.ModelInfo {
	return r.providerModels[provider]
}

type adminServiceProviderRepo struct {
	providers []*domain.Provider
}

func (r *adminServiceProviderRepo) Create(provider *domain.Provider) error  { return nil }
func (r *adminServiceProviderRepo) Update(provider *domain.Provider) error  { return nil }
func (r *adminServiceProviderRepo) Delete(tenantID uint64, id uint64) error { return nil }
func (r *adminServiceProviderRepo) GetByID(tenantID uint64, id uint64) (*domain.Provider, error) {
	return nil, domain.ErrNotFound
}
func (r *adminServiceProviderRepo) List(tenantID uint64) ([]*domain.Provider, error) {
	result := make([]*domain.Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		if provider == nil {
			continue
		}
		if tenantID != domain.TenantIDAll && provider.TenantID != tenantID {
			continue
		}
		copy := *provider
		result = append(result, &copy)
	}
	return result, nil
}

type adminServiceModelPriceRepo struct {
	nextID uint64
	prices []*domain.ModelPrice
}

func newAdminServiceModelPriceRepo(prices []*domain.ModelPrice) *adminServiceModelPriceRepo {
	repo := &adminServiceModelPriceRepo{}
	_ = repo.BatchCreate(prices)
	return repo
}

func (r *adminServiceModelPriceRepo) Create(price *domain.ModelPrice) error {
	if price.ID == 0 {
		r.nextID++
		price.ID = r.nextID
	}
	if price.CreatedAt.IsZero() {
		price.CreatedAt = time.Now()
	}
	copy := *price
	r.prices = append(r.prices, &copy)
	return nil
}

func (r *adminServiceModelPriceRepo) BatchCreate(prices []*domain.ModelPrice) error {
	for _, price := range prices {
		if err := r.Create(price); err != nil {
			return err
		}
	}
	return nil
}

func (r *adminServiceModelPriceRepo) GetByID(id uint64) (*domain.ModelPrice, error) {
	for _, price := range r.prices {
		if price.ID == id {
			copy := *price
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("model price %d not found", id)
}

func (r *adminServiceModelPriceRepo) GetCurrentByModelID(modelID string) (*domain.ModelPrice, error) {
	var best *domain.ModelPrice
	for _, price := range r.prices {
		if price.ModelID == modelID || strings.HasPrefix(modelID, price.ModelID) {
			if best == nil || len(price.ModelID) > len(best.ModelID) || price.ID > best.ID {
				copy := *price
				best = &copy
			}
		}
	}
	return best, nil
}

func (r *adminServiceModelPriceRepo) ListCurrentPrices() ([]*domain.ModelPrice, error) {
	latest := make(map[string]*domain.ModelPrice)
	for _, price := range r.prices {
		if existing := latest[price.ModelID]; existing == nil || price.ID > existing.ID {
			copy := *price
			latest[price.ModelID] = &copy
		}
	}
	result := make([]*domain.ModelPrice, 0, len(latest))
	for _, price := range latest {
		result = append(result, price)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModelID < result[j].ModelID
	})
	return result, nil
}

func (r *adminServiceModelPriceRepo) ListByModelID(modelID string) ([]*domain.ModelPrice, error) {
	var result []*domain.ModelPrice
	for _, price := range r.prices {
		if price.ModelID == modelID {
			copy := *price
			result = append(result, &copy)
		}
	}
	return result, nil
}

func (r *adminServiceModelPriceRepo) Count() (int64, error) {
	return int64(len(r.prices)), nil
}

func (r *adminServiceModelPriceRepo) Delete(id uint64) error {
	for i, price := range r.prices {
		if price.ID == id {
			r.prices = append(r.prices[:i], r.prices[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *adminServiceModelPriceRepo) Update(price *domain.ModelPrice) error {
	for i, existing := range r.prices {
		if existing.ID == price.ID {
			copy := *price
			r.prices[i] = &copy
			return nil
		}
	}
	return r.Create(price)
}

func (r *adminServiceModelPriceRepo) SoftDeleteAll() error {
	r.prices = nil
	return nil
}

func (r *adminServiceModelPriceRepo) ResetToDefaults() ([]*domain.ModelPrice, error) {
	if err := r.SoftDeleteAll(); err != nil {
		return nil, err
	}
	prices := pricing.ConvertToDBPrices(pricing.DefaultPriceTable())
	if err := r.BatchCreate(prices); err != nil {
		return nil, err
	}
	return prices, nil
}

func (r *adminServiceModelPriceRepo) hasModel(modelID string) bool {
	for _, price := range r.prices {
		if price.ModelID == modelID {
			return true
		}
	}
	return false
}

func (r *adminServiceModelPriceRepo) mustModel(t *testing.T, modelID string) *domain.ModelPrice {
	t.Helper()
	for _, price := range r.prices {
		if price.ModelID == modelID {
			return price
		}
	}
	t.Fatalf("model %q not found in prices", modelID)
	return nil
}
