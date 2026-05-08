package modelavailability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/awsl-project/maxx/internal/domain"
	"github.com/awsl-project/maxx/internal/repository"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
)

const defaultModelEndpointTimeout = 5 * time.Second

var defaultHTTPClient = &http.Client{Timeout: defaultModelEndpointTimeout}

// Registry is the CLIProxyAPI model registry surface used by Maxx.
type Registry interface {
	GetAvailableModels(handlerType string) []map[string]any
	GetAvailableModelsByProvider(provider string) []*cliproxy.ModelInfo
}

// Source collects model IDs that are actually available from configured
// providers/accounts and CLIProxyAPI's runtime registry.
type Source struct {
	ProviderRepo repository.ProviderRepository
	Registry     Registry
	HTTPClient   *http.Client
}

// CollectOptions controls which availability hints are included.
type CollectOptions struct {
	IncludeRegistry              bool
	FetchProviderModelEndpoints  bool
	IncludeProviderSupportModels bool
}

// DefaultCollectOptions returns the model-list behavior for /v1/models.
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{
		IncludeRegistry:              true,
		FetchProviderModelEndpoints:  true,
		IncludeProviderSupportModels: true,
	}
}

// Collect returns sorted, de-duplicated model IDs. Wildcards are intentionally
// ignored because pricing imports and model-list responses need concrete IDs.
func (s Source) Collect(ctx context.Context, tenantID uint64, opts CollectOptions) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := make(map[string]struct{})

	if opts.IncludeRegistry {
		s.collectRegistryModels(result)
	}

	if s.ProviderRepo != nil && (opts.FetchProviderModelEndpoints || opts.IncludeProviderSupportModels) {
		providers, err := s.ProviderRepo.List(tenantID)
		if err != nil {
			return nil, err
		}
		for _, provider := range providers {
			if opts.FetchProviderModelEndpoints {
				if err := s.collectProviderEndpointModels(ctx, result, provider); err != nil {
					return nil, err
				}
			}
			if opts.IncludeProviderSupportModels {
				collectProviderSupportModels(result, provider)
			}
		}
	}

	names := make([]string, 0, len(result))
	for name := range result {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s Source) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return defaultHTTPClient
}

func (s Source) registry() Registry {
	if s.Registry != nil {
		return s.Registry
	}
	return cliproxy.GlobalModelRegistry()
}

func (s Source) collectRegistryModels(result map[string]struct{}) {
	registry := s.registry()
	if registry == nil {
		return
	}

	for _, handlerType := range []string{"openai", "claude", "gemini"} {
		for _, model := range registry.GetAvailableModels(handlerType) {
			if model == nil {
				continue
			}
			if id, _ := model["id"].(string); id != "" {
				add(result, id)
			}
			if name, _ := model["name"].(string); name != "" {
				add(result, name)
			}
		}
	}

	for _, provider := range []string{
		"codex",
		"antigravity",
		"claude",
		"gemini",
		"gemini-cli",
		"aistudio",
		"vertex",
		"openai",
		"openai-compatibility",
		"kimi",
	} {
		for _, model := range registry.GetAvailableModelsByProvider(provider) {
			if model == nil {
				continue
			}
			add(result, model.ID)
			add(result, model.Name)
		}
	}
}

func collectProviderSupportModels(result map[string]struct{}, provider *domain.Provider) {
	if provider == nil {
		return
	}
	for _, name := range provider.SupportModels {
		add(result, name)
	}
}

func (s Source) collectProviderEndpointModels(ctx context.Context, result map[string]struct{}, provider *domain.Provider) error {
	for _, endpoint := range providerModelEndpoints(provider) {
		models, err := s.fetchModelsEndpoint(ctx, endpoint)
		if err != nil {
			return err
		}
		for _, model := range models {
			add(result, model)
		}
	}
	return nil
}

type modelEndpoint struct {
	URL    string
	APIKey string
}

func providerModelEndpoints(provider *domain.Provider) []modelEndpoint {
	if provider == nil || provider.Config == nil {
		return nil
	}

	var endpoints []modelEndpoint
	seen := make(map[string]struct{})
	addBase := func(baseURL, apiKey string) {
		baseURL = strings.TrimSpace(baseURL)
		if baseURL == "" {
			return
		}
		for _, path := range []string{"/v1/models", "/v1beta/models"} {
			endpointURL := joinURLPath(baseURL, path)
			key := endpointURL + "\x00" + apiKey
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			endpoints = append(endpoints, modelEndpoint{URL: endpointURL, APIKey: apiKey})
		}
	}

	if provider.Config == nil {
		return endpoints
	}
	if provider.Config.Custom != nil {
		apiKey := strings.TrimSpace(provider.Config.Custom.APIKey)
		addBase(provider.Config.Custom.BaseURL, apiKey)
		for _, baseURL := range provider.Config.Custom.ClientBaseURL {
			addBase(baseURL, apiKey)
		}
	}

	return endpoints
}

func joinURLPath(baseURL, path string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + path
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return strings.TrimRight(baseURL, "/") + path
}

func (s Source) fetchModelsEndpoint(ctx context.Context, endpoint modelEndpoint) ([]string, error) {
	if endpoint.URL == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create provider models request %s: %w", endpoint.URL, err)
	}
	req.Header.Set("Accept", "application/json")
	if endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch provider models %s: %w", endpoint.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch provider models %s: status %d", endpoint.URL, resp.StatusCode)
	}

	return parseModelsEndpoint(io.LimitReader(resp.Body, 4<<20))
}

func parseModelsEndpoint(r io.Reader) ([]string, error) {
	var payload struct {
		Data   []map[string]any `json:"data"`
		Models []map[string]any `json:"models"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode provider models response: %w", err)
	}

	var models []string
	for _, item := range payload.Data {
		models = append(models, stringFields(item, "id", "name", "baseModelId")...)
	}
	for _, item := range payload.Models {
		models = append(models, stringFields(item, "id", "name", "baseModelId")...)
	}
	return models, nil
}

func stringFields(item map[string]any, keys ...string) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, ok := item[key].(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func add(result map[string]struct{}, modelID string) {
	id := NormalizeModelID(modelID)
	if id == "" || strings.Contains(id, "*") {
		return
	}
	result[id] = struct{}{}
}

// NormalizeModelID canonicalizes IDs returned from OpenAI-, Claude-, and
// Gemini-style model endpoints into the plain model IDs Maxx stores/prices.
func NormalizeModelID(modelID string) string {
	id := strings.TrimSpace(modelID)
	id = strings.TrimPrefix(id, "models/")
	id = strings.TrimSpace(id)
	return id
}

func clientIDForProvider(providerID uint64) string {
	return fmt.Sprintf("maxx-provider-%d", providerID)
}

// RegisterCLIProxyCodexProvider mirrors the connected Codex account into the
// CLIProxyAPI model registry so /v1/models and pricing updates share the same
// availability source.
func RegisterCLIProxyCodexProvider(provider *domain.Provider) {
	if provider == nil || provider.ID == 0 || provider.Config == nil || provider.Config.Codex == nil {
		return
	}
	plan := normalizeCodexPlan(provider.Config.Codex.PlanType)
	ids := codexModelsByPlan[plan]
	if len(ids) == 0 {
		ids = codexModelsByPlan["pro"]
	}
	cliproxy.GlobalModelRegistry().RegisterClient(clientIDForProvider(provider.ID), "codex", modelInfos(ids, "openai", "openai"))
}

// RegisterCLIProxyAntigravityProvider mirrors the connected Antigravity account
// into the CLIProxyAPI model registry.
func RegisterCLIProxyAntigravityProvider(provider *domain.Provider) {
	if provider == nil || provider.ID == 0 {
		return
	}
	cliproxy.GlobalModelRegistry().RegisterClient(clientIDForProvider(provider.ID), "antigravity", modelInfos(antigravityModels, "antigravity", "antigravity"))
}

// UnregisterCLIProxyProvider removes a provider registration from the shared
// CLIProxyAPI registry. It is safe to call for providers that were never registered.
func UnregisterCLIProxyProvider(providerID uint64) {
	if providerID == 0 {
		return
	}
	cliproxy.GlobalModelRegistry().UnregisterClient(clientIDForProvider(providerID))
}

func modelInfos(ids []string, ownedBy, modelType string) []*cliproxy.ModelInfo {
	created := time.Now().Unix()
	models := make([]*cliproxy.ModelInfo, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = NormalizeModelID(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		models = append(models, &cliproxy.ModelInfo{
			ID:          id,
			Object:      "model",
			Created:     created,
			OwnedBy:     ownedBy,
			Type:        modelType,
			DisplayName: id,
			Version:     id,
		})
	}
	return models
}

func normalizeCodexPlan(planType string) string {
	plan := strings.ToLower(strings.TrimSpace(planType))
	switch {
	case strings.Contains(plan, "free"):
		return "free"
	case strings.Contains(plan, "plus"):
		return "plus"
	case strings.Contains(plan, "team"), strings.Contains(plan, "business"), strings.Contains(plan, "go"):
		return "team"
	case strings.Contains(plan, "pro"):
		return "pro"
	default:
		return "pro"
	}
}

var codexModelsByPlan = map[string][]string{
	"free": {
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"codex-auto-review",
		"gpt-image-2",
	},
	"plus": {
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"codex-auto-review",
		"gpt-image-2",
	},
	"team": {
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"codex-auto-review",
		"gpt-image-2",
	},
	"pro": {
		"gpt-5.2",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"codex-auto-review",
		"gpt-image-2",
	},
}

var antigravityModels = []string{
	"claude-opus-4-6-thinking",
	"claude-sonnet-4-6",
	"gemini-3-flash",
	"gemini-3-pro-high",
	"gemini-3-pro-low",
	"gemini-3.1-flash-image",
	"gemini-3.1-pro-high",
	"gemini-3.1-pro-low",
	"gpt-oss-120b-medium",
	"gemini-3.1-flash-lite",
}
