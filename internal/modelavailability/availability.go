package modelavailability

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/awsl-project/maxx/internal/domain"
	"github.com/awsl-project/maxx/internal/repository"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
)

// Registry is the CLIProxyAPI model registry surface used by Maxx.
type Registry interface {
	GetAvailableModels(handlerType string) []map[string]any
	GetAvailableModelsByProvider(provider string) []*cliproxy.ModelInfo
}

// Source collects model IDs that are actually available from configured
// providers/accounts and CLIProxyAPI's runtime registry.
type Source struct {
	ResponseModelRepo repository.ResponseModelRepository
	ProviderRepo      repository.ProviderRepository
	ModelMappingRepo  repository.ModelMappingRepository
	Registry          Registry
}

// CollectOptions controls which availability hints are included.
type CollectOptions struct {
	IncludeRegistry       bool
	IncludeResponseModels bool
	IncludeProviderHints  bool
	IncludeModelMappings  bool
}

// DefaultCollectOptions returns the model-list behavior for /v1/models.
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{
		IncludeRegistry:       true,
		IncludeResponseModels: true,
		IncludeProviderHints:  true,
		IncludeModelMappings:  true,
	}
}

// Collect returns sorted, de-duplicated model IDs. Wildcards are intentionally
// ignored because pricing imports and model-list responses need concrete IDs.
func (s Source) Collect(tenantID uint64, opts CollectOptions) ([]string, error) {
	result := make(map[string]struct{})

	if opts.IncludeRegistry {
		s.collectRegistryModels(result)
	}

	if opts.IncludeResponseModels && s.ResponseModelRepo != nil {
		names, err := s.ResponseModelRepo.ListNames()
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			add(result, name)
		}
	}

	if opts.IncludeProviderHints && s.ProviderRepo != nil {
		providers, err := s.ProviderRepo.List(tenantID)
		if err != nil {
			return nil, err
		}
		for _, provider := range providers {
			collectProviderHints(result, provider)
		}
	}

	if opts.IncludeModelMappings && s.ModelMappingRepo != nil {
		mappings, err := s.ModelMappingRepo.ListEnabled(tenantID)
		if err != nil {
			return nil, err
		}
		for _, mapping := range mappings {
			if mapping == nil {
				continue
			}
			add(result, mapping.Target)
			add(result, mapping.Pattern)
		}
	}

	names := make([]string, 0, len(result))
	for name := range result {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
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

func collectProviderHints(result map[string]struct{}, provider *domain.Provider) {
	if provider == nil {
		return
	}
	for _, name := range provider.SupportModels {
		add(result, name)
	}
	if provider.Config == nil {
		return
	}
	if provider.Config.Custom != nil {
		collectMapping(result, provider.Config.Custom.ModelMapping)
	}
	if provider.Config.Codex != nil {
		collectMapping(result, provider.Config.Codex.ModelMapping)
	}
	if provider.Config.Antigravity != nil {
		collectMapping(result, provider.Config.Antigravity.ModelMapping)
	}
	if provider.Config.Claude != nil {
		collectMapping(result, provider.Config.Claude.ModelMapping)
	}
	if provider.Config.Bedrock != nil {
		collectMapping(result, provider.Config.Bedrock.ModelMapping)
	}
	if provider.Config.Kiro != nil {
		collectMapping(result, provider.Config.Kiro.ModelMapping)
	}
	if provider.Config.CLIProxyAPIAntigravity != nil {
		collectMapping(result, provider.Config.CLIProxyAPIAntigravity.ModelMapping)
	}
	if provider.Config.CLIProxyAPICodex != nil {
		collectMapping(result, provider.Config.CLIProxyAPICodex.ModelMapping)
	}
}

func collectMapping(result map[string]struct{}, mapping map[string]string) {
	for pattern, target := range mapping {
		add(result, pattern)
		add(result, target)
	}
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
