package modelavailability

import (
	"testing"

	"github.com/awsl-project/maxx/internal/domain"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
)

type fakeRegistry struct {
	handlerModels  map[string][]map[string]any
	providerModels map[string][]*cliproxy.ModelInfo
}

func (r fakeRegistry) GetAvailableModels(handlerType string) []map[string]any {
	return r.handlerModels[handlerType]
}

func (r fakeRegistry) GetAvailableModelsByProvider(provider string) []*cliproxy.ModelInfo {
	return r.providerModels[provider]
}

func TestSourceCollectUsesCLIProxyRegistryAndProviderHints(t *testing.T) {
	source := Source{
		Registry: fakeRegistry{
			handlerModels: map[string][]map[string]any{
				"openai": {
					{"id": "gpt-registry"},
					{"name": "models/gemini-from-name"},
				},
			},
			providerModels: map[string][]*cliproxy.ModelInfo{
				"codex": {&cliproxy.ModelInfo{ID: "gpt-codex-provider"}},
			},
		},
		ProviderRepo: &fakeAvailabilityProviderRepo{providers: []*domain.Provider{{
			SupportModels: []string{"claude-provider", "*"},
			Config: &domain.ProviderConfig{Codex: &domain.ProviderConfigCodex{ModelMapping: map[string]string{
				"gpt-request": "gpt-mapped",
			}}},
		}}},
	}

	names, err := source.Collect(domain.DefaultTenantID, DefaultCollectOptions())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, want := range []string{"gpt-registry", "gemini-from-name", "gpt-codex-provider", "claude-provider", "gpt-request", "gpt-mapped"} {
		if !containsName(names, want) {
			t.Fatalf("expected %q in collected names: %v", want, names)
		}
	}
	if containsName(names, "*") {
		t.Fatalf("did not expect wildcard in collected names: %v", names)
	}
}

func TestRegisterCLIProxyCodexProviderUsesPlainModelIDs(t *testing.T) {
	provider := &domain.Provider{
		ID:   987654321,
		Name: "codex-test",
		Config: &domain.ProviderConfig{Codex: &domain.ProviderConfigCodex{
			UseCLIProxyAPI: true,
			PlanType:       "chatgptplusplan",
		}},
	}
	RegisterCLIProxyCodexProvider(provider)
	t.Cleanup(func() { UnregisterCLIProxyProvider(provider.ID) })

	models := cliproxy.GlobalModelRegistry().GetAvailableModelsByProvider("codex")
	if len(models) == 0 {
		t.Fatal("expected codex models registered")
	}
	for _, model := range models {
		if model == nil {
			continue
		}
		if model.ID == "openai/gpt-5.4" || model.ID == "models/gpt-5.4" {
			t.Fatalf("expected plain model IDs, got provider-qualified %q", model.ID)
		}
	}
	if !containsModelInfo(models, "gpt-5.4") {
		t.Fatalf("expected gpt-5.4 in registered codex models")
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func containsModelInfo(models []*cliproxy.ModelInfo, want string) bool {
	for _, model := range models {
		if model != nil && model.ID == want {
			return true
		}
	}
	return false
}

type fakeAvailabilityProviderRepo struct {
	providers []*domain.Provider
}

func (r *fakeAvailabilityProviderRepo) Create(provider *domain.Provider) error  { return nil }
func (r *fakeAvailabilityProviderRepo) Update(provider *domain.Provider) error  { return nil }
func (r *fakeAvailabilityProviderRepo) Delete(tenantID uint64, id uint64) error { return nil }
func (r *fakeAvailabilityProviderRepo) GetByID(tenantID uint64, id uint64) (*domain.Provider, error) {
	return nil, domain.ErrNotFound
}
func (r *fakeAvailabilityProviderRepo) List(tenantID uint64) ([]*domain.Provider, error) {
	return append([]*domain.Provider(nil), r.providers...), nil
}
