package modelavailability

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestSourceCollectUsesCLIProxyRegistryProviderEndpointsAndSupportModels(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want bearer API key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"gpt-provider-endpoint"},{"name":"models/gemini-from-provider"}]}`)
	}))
	defer modelServer.Close()

	source := Source{
		Registry: fakeRegistry{
			providerModels: map[string][]*cliproxy.ModelInfo{
				"codex": {&cliproxy.ModelInfo{ID: "gpt-codex-provider"}},
			},
		},
		ProviderRepo: &fakeAvailabilityProviderRepo{providers: []*domain.Provider{
			{
				Type: "codex",
				Config: &domain.ProviderConfig{Codex: &domain.ProviderConfigCodex{
					UseCLIProxyAPI: true,
					ModelMapping: map[string]string{
						"gpt-request": "gpt-mapped",
					},
				}},
			},
			{
				Type:          "custom",
				SupportModels: []string{"claude-provider", "*"},
				Config: &domain.ProviderConfig{Custom: &domain.ProviderConfigCustom{
					BaseURL: modelServer.URL,
					APIKey:  "sk-test",
				}},
			},
		}},
	}

	names, err := source.Collect(context.Background(), domain.DefaultTenantID, DefaultCollectOptions())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, want := range []string{"gpt-codex-provider", "claude-provider", "gpt-provider-endpoint", "gemini-from-provider"} {
		if !containsName(names, want) {
			t.Fatalf("expected %q in collected names: %v", want, names)
		}
	}
	if containsName(names, "gpt-mapped") || containsName(names, "gpt-request") {
		t.Fatalf("did not expect model mappings to advertise availability: %v", names)
	}
	if containsName(names, "*") {
		t.Fatalf("did not expect wildcard in collected names: %v", names)
	}
}

func TestSourceCollectDoesNotUseCLIProxyRegistryWithoutProviders(t *testing.T) {
	source := Source{
		Registry: fakeRegistry{providerModels: map[string][]*cliproxy.ModelInfo{
			"codex": {&cliproxy.ModelInfo{ID: "gpt-registry-without-provider"}},
		}},
	}

	names, err := source.Collect(context.Background(), domain.DefaultTenantID, DefaultCollectOptions())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected no availability without configured providers, got %v", names)
	}
}

func TestSourceCollectDoesNotUseModelMappingsWithoutProviderAvailability(t *testing.T) {
	source := Source{
		ProviderRepo: &fakeAvailabilityProviderRepo{providers: []*domain.Provider{{
			Type: "codex",
			Config: &domain.ProviderConfig{Codex: &domain.ProviderConfigCodex{ModelMapping: map[string]string{
				"gpt-request": "gpt-mapped",
			}}},
		}}},
		Registry: fakeRegistry{},
	}

	names, err := source.Collect(context.Background(), domain.DefaultTenantID, DefaultCollectOptions())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected no availability from mappings alone, got %v", names)
	}
}

func TestProviderModelEndpointURLsRespectVersionedBaseURL(t *testing.T) {
	baseURL := "https://provider.example.test/openai/v1"
	got := modelEndpointURLs(baseURL)
	want := []string{"https://provider.example.test/openai/v1/models"}
	if len(got) != len(want) {
		t.Fatalf("endpoint count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("endpoint[%d] = %q, want %q", i, got[i], want[i])
		}
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
