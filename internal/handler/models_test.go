package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/awsl-project/maxx/internal/domain"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
)

type fakeResponseModelRepo struct {
	names []string
	err   error
}

func (f *fakeResponseModelRepo) Upsert(name string) error               { return nil }
func (f *fakeResponseModelRepo) BatchUpsert(names []string) error       { return nil }
func (f *fakeResponseModelRepo) List() ([]*domain.ResponseModel, error) { return nil, f.err }
func (f *fakeResponseModelRepo) ListNames() ([]string, error) {
	return append([]string(nil), f.names...), f.err
}

type fakeProviderRepo struct {
	providers []*domain.Provider
	err       error
}

func (f *fakeProviderRepo) Create(provider *domain.Provider) error  { return nil }
func (f *fakeProviderRepo) Update(provider *domain.Provider) error  { return nil }
func (f *fakeProviderRepo) Delete(tenantID uint64, id uint64) error { return nil }
func (f *fakeProviderRepo) GetByID(tenantID uint64, id uint64) (*domain.Provider, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeProviderRepo) List(tenantID uint64) ([]*domain.Provider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]*domain.Provider(nil), f.providers...), nil
}

type fakeModelMappingRepo struct {
	mappings []*domain.ModelMapping
	err      error
}

func (f *fakeModelMappingRepo) Create(mapping *domain.ModelMapping) error { return nil }
func (f *fakeModelMappingRepo) Update(mapping *domain.ModelMapping) error { return nil }
func (f *fakeModelMappingRepo) Delete(tenantID uint64, id uint64) error   { return nil }
func (f *fakeModelMappingRepo) GetByID(tenantID uint64, id uint64) (*domain.ModelMapping, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeModelMappingRepo) List(tenantID uint64) ([]*domain.ModelMapping, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]*domain.ModelMapping(nil), f.mappings...), nil
}
func (f *fakeModelMappingRepo) ListEnabled(tenantID uint64) ([]*domain.ModelMapping, error) {
	return f.List(tenantID)
}
func (f *fakeModelMappingRepo) ListByClientType(tenantID uint64, clientType domain.ClientType) ([]*domain.ModelMapping, error) {
	return f.List(tenantID)
}
func (f *fakeModelMappingRepo) ListByQuery(tenantID uint64, query *domain.ModelMappingQuery) ([]*domain.ModelMapping, error) {
	return f.List(tenantID)
}
func (f *fakeModelMappingRepo) Count(tenantID uint64) (int, error) { return len(f.mappings), f.err }
func (f *fakeModelMappingRepo) DeleteAll(tenantID uint64) error    { return nil }
func (f *fakeModelMappingRepo) ClearAll(tenantID uint64) error     { return nil }
func (f *fakeModelMappingRepo) SeedDefaults(tenantID uint64) error { return nil }

type fakeModelPriceRepo struct {
	prices []*domain.ModelPrice
	err    error
}

func (f *fakeModelPriceRepo) Create(price *domain.ModelPrice) error { return nil }
func (f *fakeModelPriceRepo) BatchCreate(prices []*domain.ModelPrice) error {
	return nil
}
func (f *fakeModelPriceRepo) GetByID(id uint64) (*domain.ModelPrice, error) { return nil, nil }
func (f *fakeModelPriceRepo) GetCurrentByModelID(modelID string) (*domain.ModelPrice, error) {
	return nil, nil
}
func (f *fakeModelPriceRepo) ListCurrentPrices() ([]*domain.ModelPrice, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]*domain.ModelPrice(nil), f.prices...), nil
}
func (f *fakeModelPriceRepo) ListByModelID(modelID string) ([]*domain.ModelPrice, error) {
	return nil, nil
}
func (f *fakeModelPriceRepo) Count() (int64, error)                          { return int64(len(f.prices)), f.err }
func (f *fakeModelPriceRepo) Delete(id uint64) error                         { return nil }
func (f *fakeModelPriceRepo) Update(price *domain.ModelPrice) error          { return nil }
func (f *fakeModelPriceRepo) SoftDeleteAll() error                           { return nil }
func (f *fakeModelPriceRepo) ResetToDefaults() ([]*domain.ModelPrice, error) { return nil, nil }

func containsModel(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestCollectModelNames(t *testing.T) {
	responseRepo := &fakeResponseModelRepo{names: []string{"gpt-1", "gpt-2"}}
	providerRepo := &fakeProviderRepo{
		providers: []*domain.Provider{
			{SupportModels: []string{"gpt-3", "*", " "}},
		},
	}
	mappingRepo := &fakeModelMappingRepo{
		mappings: []*domain.ModelMapping{
			{Pattern: "gpt-4", Target: "gpt-4o"},
			{Pattern: "gpt-*", Target: "gpt-5"},
		},
	}

	handler := NewModelsHandler(responseRepo, providerRepo, mappingRepo, nil)
	names, err := handler.collectModelNames(context.Background(), 0)
	if err != nil {
		t.Fatalf("collectModelNames error: %v", err)
	}

	want := []string{"gpt-3"}
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("model count = %d, want %d", len(names), len(want))
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("names[%d] = %q, want %q", i, names[i], name)
		}
	}
}

func TestModelsHandlerFormats(t *testing.T) {
	responseRepo := &fakeResponseModelRepo{names: []string{"gpt-1"}}
	providerRepo := &fakeProviderRepo{providers: []*domain.Provider{{SupportModels: []string{"gpt-1"}}}}
	handler := NewModelsHandler(responseRepo, providerRepo, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("User-Agent", "claude-cli/2.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var claudeResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &claudeResp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := claudeResp["has_more"]; !ok {
		t.Fatalf("claude response missing has_more")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var openaiResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &openaiResp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if openaiResp["object"] != "list" {
		t.Fatalf("openai response object = %v, want list", openaiResp["object"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var geminiResp struct {
		Models []struct {
			Name                       string   `json:"name"`
			BaseModelID                string   `json:"baseModelId"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &geminiResp); err != nil {
		t.Fatalf("invalid Gemini payload: %v", err)
	}
	if len(geminiResp.Models) != 1 {
		t.Fatalf("Gemini model count = %d, want 1", len(geminiResp.Models))
	}
	if geminiResp.Models[0].Name != "models/gpt-1" {
		t.Fatalf("Gemini model name = %q, want models/gpt-1", geminiResp.Models[0].Name)
	}
	if geminiResp.Models[0].BaseModelID != "gpt-1" {
		t.Fatalf("Gemini baseModelId = %q, want gpt-1", geminiResp.Models[0].BaseModelID)
	}
	if !containsModel(geminiResp.Models[0].SupportedGenerationMethods, "generateContent") {
		t.Fatalf("Gemini response missing generateContent support")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	req.Header.Set("User-Agent", "claude-cli/2.0")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var geminiWithClaudeUA struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &geminiWithClaudeUA); err != nil {
		t.Fatalf("invalid Gemini payload with Claude UA: %v", err)
	}
	if len(geminiWithClaudeUA.Models) != 1 || geminiWithClaudeUA.Models[0].Name != "models/gpt-1" {
		t.Fatalf("Gemini path with Claude UA returned wrong payload: %+v", geminiWithClaudeUA.Models)
	}
}

func TestModelsHandlerUsesCLIProxyAPIRegistryModels(t *testing.T) {
	registry := cliproxy.GlobalModelRegistry()
	registry.RegisterClient("models-handler-test-codex", "codex", []*cliproxy.ModelInfo{{ID: "gpt-registry-live"}})
	registry.RegisterClient("models-handler-test-antigravity", "antigravity", []*cliproxy.ModelInfo{{ID: "claude-registry-live"}})
	t.Cleanup(func() {
		registry.UnregisterClient("models-handler-test-codex")
		registry.UnregisterClient("models-handler-test-antigravity")
	})

	providerRepo := &fakeProviderRepo{providers: []*domain.Provider{
		{
			Type: "codex",
			Config: &domain.ProviderConfig{Codex: &domain.ProviderConfigCodex{
				UseCLIProxyAPI: true,
			}},
		},
		{
			Type: "antigravity",
			Config: &domain.ProviderConfig{Antigravity: &domain.ProviderConfigAntigravity{
				UseCLIProxyAPI: true,
			}},
		},
	}}
	handler := NewModelsHandler(nil, providerRepo, nil, nil)

	openAIReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	openAIReq.Header.Set("User-Agent", "codex_cli_rs/0.98.0")
	openAIRec := httptest.NewRecorder()
	handler.ServeHTTP(openAIRec, openAIReq)
	if openAIRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", openAIRec.Code)
	}
	var openAIPayload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(openAIRec.Body.Bytes(), &openAIPayload); err != nil {
		t.Fatalf("invalid openai payload: %v", err)
	}
	openAIIDs := make([]string, 0, len(openAIPayload.Data))
	for _, item := range openAIPayload.Data {
		openAIIDs = append(openAIIDs, item.ID)
	}
	if !containsModel(openAIIDs, "gpt-registry-live") {
		t.Fatalf("expected gpt-registry-live in model list")
	}
	if !containsModel(openAIIDs, "claude-registry-live") {
		t.Fatalf("expected claude-registry-live in model list")
	}

	claudeReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	claudeReq.Header.Set("User-Agent", "claude-cli/2.1.17")
	claudeRec := httptest.NewRecorder()
	handler.ServeHTTP(claudeRec, claudeReq)
	if claudeRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", claudeRec.Code)
	}
	var claudePayload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(claudeRec.Body.Bytes(), &claudePayload); err != nil {
		t.Fatalf("invalid claude payload: %v", err)
	}
	claudeIDs := make([]string, 0, len(claudePayload.Data))
	for _, item := range claudePayload.Data {
		claudeIDs = append(claudeIDs, item.ID)
	}
	if !containsModel(claudeIDs, "claude-registry-live") {
		t.Fatalf("expected claude-registry-live in claude-format model list")
	}
	if !containsModel(claudeIDs, "gpt-registry-live") {
		t.Fatalf("expected gpt-registry-live in claude-format model list")
	}
}

func TestModelsHandlerDoesNotUseCLIProxyAPIRegistryWithoutProviders(t *testing.T) {
	registry := cliproxy.GlobalModelRegistry()
	registry.RegisterClient("models-handler-no-provider", "codex", []*cliproxy.ModelInfo{{ID: "gpt-registry-orphan"}})
	t.Cleanup(func() {
		registry.UnregisterClient("models-handler-no-provider")
	})

	handler := NewModelsHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid payload: %v", err)
	}
	for _, item := range payload.Data {
		if item.ID == "gpt-registry-orphan" {
			t.Fatalf("did not expect registry model without a configured provider: %+v", payload.Data)
		}
	}
}

func TestModelsHandlerDoesNotUseCurrentModelPricesForAvailability(t *testing.T) {
	priceRepo := &fakeModelPriceRepo{
		prices: []*domain.ModelPrice{
			{ModelID: "gpt-live-from-pricing"},
			{ModelID: "claude-live-from-pricing"},
			{ModelID: "*wildcard-skip"},
		},
	}
	handler := NewModelsHandler(nil, nil, nil, priceRepo)

	openAIReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	openAIReq.Header.Set("User-Agent", "codex_cli_rs/0.98.0")
	openAIRec := httptest.NewRecorder()
	handler.ServeHTTP(openAIRec, openAIReq)
	if openAIRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", openAIRec.Code)
	}
	var openAIPayload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(openAIRec.Body.Bytes(), &openAIPayload); err != nil {
		t.Fatalf("invalid openai payload: %v", err)
	}
	openAIIDs := make([]string, 0, len(openAIPayload.Data))
	for _, item := range openAIPayload.Data {
		openAIIDs = append(openAIIDs, item.ID)
	}
	if containsModel(openAIIDs, "gpt-live-from-pricing") {
		t.Fatalf("did not expect pricing-only model in model availability list")
	}
	if containsModel(openAIIDs, "claude-live-from-pricing") || containsModel(openAIIDs, "*wildcard-skip") {
		t.Fatalf("did not expect wildcard price row in model list")
	}
}
