package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// ModelsDevDefaultURL is the public models.dev catalog endpoint.
	ModelsDevDefaultURL = "https://models.dev/api.json"

	modelsDevPriceTableVersion = "models.dev"
	modelsDevMaxResponseBytes  = 32 << 20
	modelsDevUserAgent         = "Mozilla/5.0 (compatible; maxx-pricing-updater/1.0; +https://github.com/awsl-project/maxx)"
)

var (
	defaultModelsDevHTTPClient = &http.Client{Timeout: 20 * time.Second}

	modelsDevProviderPriority = map[string]int{
		"openai":        0,
		"anthropic":     1,
		"google":        2,
		"google-vertex": 3,
		"xai":           4,
		"deepseek":      5,
		"mistral":       6,
		"cohere":        7,
		"perplexity":    8,
		"groq":          9,
	}
)

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID   string         `json:"id"`
	Cost *modelsDevCost `json:"cost"`
}

type modelsDevCost struct {
	Input           *float64                 `json:"input"`
	Output          *float64                 `json:"output"`
	CacheRead       *float64                 `json:"cache_read"`
	CacheWrite      *float64                 `json:"cache_write"`
	ContextOver200K *modelsDevContextPricing `json:"context_over_200k"`
}

type modelsDevContextPricing struct {
	Input  *float64 `json:"input"`
	Output *float64 `json:"output"`
}

// FetchModelsDevPriceTable downloads and parses the models.dev pricing catalog.
func FetchModelsDevPriceTable(ctx context.Context, client *http.Client, endpoint string) (*PriceTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = defaultModelsDevHTTPClient
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = ModelsDevDefaultURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create models.dev request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", modelsDevUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev pricing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch models.dev pricing: status %d", resp.StatusCode)
	}

	return ParseModelsDevPriceTable(io.LimitReader(resp.Body, modelsDevMaxResponseBytes))
}

// ParseModelsDevPriceTable converts a models.dev API response into a PriceTable.
func ParseModelsDevPriceTable(r io.Reader) (*PriceTable, error) {
	var providers map[string]modelsDevProvider
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&providers); err != nil {
		return nil, fmt.Errorf("decode models.dev pricing: %w", err)
	}

	pt := NewPriceTable(modelsDevPriceTableVersion)
	for _, providerID := range sortedModelsDevProviders(providers) {
		provider := providers[providerID]
		modelIDs := make([]string, 0, len(provider.Models))
		for modelKey := range provider.Models {
			modelIDs = append(modelIDs, modelKey)
		}
		sort.Strings(modelIDs)

		for _, modelKey := range modelIDs {
			model := provider.Models[modelKey]
			pricing := model.modelsDevPricing(modelKey)
			if pricing == nil {
				continue
			}
			if _, exists := pt.Models[pricing.ModelID]; exists {
				continue
			}
			pt.Set(pricing)
		}
	}

	if len(pt.Models) == 0 {
		return nil, errors.New("models.dev pricing contained no priced models")
	}
	return pt, nil
}

func sortedModelsDevProviders(providers map[string]modelsDevProvider) []string {
	ids := make([]string, 0, len(providers))
	for providerID := range providers {
		ids = append(ids, providerID)
	}
	sort.Slice(ids, func(i, j int) bool {
		leftPriority, leftKnown := modelsDevProviderPriority[ids[i]]
		rightPriority, rightKnown := modelsDevProviderPriority[ids[j]]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (m modelsDevModel) modelsDevPricing(modelKey string) *ModelPricing {
	if m.Cost == nil || m.Cost.Input == nil || m.Cost.Output == nil {
		return nil
	}

	modelID := strings.TrimSpace(m.ID)
	if modelID == "" {
		modelID = strings.TrimSpace(modelKey)
	}
	if modelID == "" {
		return nil
	}

	pricing := &ModelPricing{
		ModelID:          modelID,
		InputPriceMicro:  dollarsPerMillionToMicro(*m.Cost.Input),
		OutputPriceMicro: dollarsPerMillionToMicro(*m.Cost.Output),
	}
	if m.Cost.CacheRead != nil {
		pricing.CacheReadPriceMicro = dollarsPerMillionToMicro(*m.Cost.CacheRead)
	}
	if m.Cost.CacheWrite != nil {
		cacheWrite := dollarsPerMillionToMicro(*m.Cost.CacheWrite)
		pricing.Cache5mWritePriceMicro = cacheWrite
		pricing.Cache1hWritePriceMicro = cacheWrite
	}
	if m.Cost.ContextOver200K != nil {
		applyModelsDevContextPricing(pricing, m.Cost.ContextOver200K)
	}

	return pricing
}

func applyModelsDevContextPricing(pricing *ModelPricing, contextPricing *modelsDevContextPricing) {
	if pricing == nil || contextPricing == nil {
		return
	}

	inputNum, inputDenom := premiumFraction(pricing.InputPriceMicro, contextPricing.Input)
	outputNum, outputDenom := premiumFraction(pricing.OutputPriceMicro, contextPricing.Output)
	if inputNum == 0 && outputNum == 0 {
		return
	}

	pricing.Has1MContext = true
	pricing.Context1MThreshold = 200_000
	if inputNum > 0 {
		pricing.InputPremiumNum = inputNum
		pricing.InputPremiumDenom = inputDenom
	}
	if outputNum > 0 {
		pricing.OutputPremiumNum = outputNum
		pricing.OutputPremiumDenom = outputDenom
	}
}

func premiumFraction(baseMicro uint64, premiumDollarsPerMillion *float64) (uint64, uint64) {
	if baseMicro == 0 || premiumDollarsPerMillion == nil {
		return 0, 0
	}
	premiumMicro := dollarsPerMillionToMicro(*premiumDollarsPerMillion)
	if premiumMicro == 0 {
		return 0, 0
	}

	const denom = uint64(1_000_000)
	num := uint64(math.Round(float64(premiumMicro) / float64(baseMicro) * float64(denom)))
	if num == 0 {
		return 0, 0
	}
	divisor := gcd(num, denom)
	return num / divisor, denom / divisor
}

func dollarsPerMillionToMicro(price float64) uint64 {
	if price <= 0 {
		return 0
	}
	return uint64(math.Round(price * 1_000_000))
}

func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}
