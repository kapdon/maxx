package pricing

import (
	"strings"
	"testing"
)

func TestParseModelsDevPriceTable(t *testing.T) {
	body := `{
		"openai": {
			"models": {
				"gpt-test": {
					"id": "gpt-test",
					"cost": {
						"input": 1.25,
						"output": 10,
						"cache_read": 0.125,
						"context_over_200k": {
							"input": 2.5,
							"output": 15
						}
					}
				}
			}
		},
		"anthropic": {
			"models": {
				"claude-test": {
					"id": "claude-test",
					"cost": {
						"input": 3,
						"output": 15,
						"cache_read": 0.3,
						"cache_write": 3.75
					}
				}
			}
		}
	}`

	table, err := ParseModelsDevPriceTable(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseModelsDevPriceTable() error = %v", err)
	}

	gpt := table.Get("gpt-test")
	if gpt == nil {
		t.Fatal("expected gpt-test pricing")
	}
	if gpt.InputPriceMicro != 1_250_000 || gpt.OutputPriceMicro != 10_000_000 {
		t.Fatalf("gpt-test base prices = %d/%d", gpt.InputPriceMicro, gpt.OutputPriceMicro)
	}
	if gpt.CacheReadPriceMicro != 125_000 {
		t.Fatalf("gpt-test cache read = %d, want 125000", gpt.CacheReadPriceMicro)
	}
	if !gpt.Has1MContext {
		t.Fatal("expected context_over_200k to enable 1M context pricing")
	}
	if gpt.Context1MThreshold != 200_000 {
		t.Fatalf("context threshold = %d, want 200000", gpt.Context1MThreshold)
	}
	if gpt.InputPremiumNum != 2 || gpt.InputPremiumDenom != 1 {
		t.Fatalf("input premium = %d/%d, want 2/1", gpt.InputPremiumNum, gpt.InputPremiumDenom)
	}
	if gpt.OutputPremiumNum != 3 || gpt.OutputPremiumDenom != 2 {
		t.Fatalf("output premium = %d/%d, want 3/2", gpt.OutputPremiumNum, gpt.OutputPremiumDenom)
	}

	claude := table.Get("claude-test")
	if claude == nil {
		t.Fatal("expected claude-test pricing")
	}
	if claude.Cache5mWritePriceMicro != 3_750_000 || claude.Cache1hWritePriceMicro != 3_750_000 {
		t.Fatalf("claude-test cache write = %d/%d, want 3750000/3750000", claude.Cache5mWritePriceMicro, claude.Cache1hWritePriceMicro)
	}
}

func TestParseModelsDevPriceTablePrefersCanonicalProviderOnDuplicateModelID(t *testing.T) {
	body := `{
		"zzz-proxy": {
			"models": {
				"gpt-duplicate": {
					"id": "gpt-duplicate",
					"cost": {"input": 9, "output": 99}
				}
			}
		},
		"openai": {
			"models": {
				"gpt-duplicate": {
					"id": "gpt-duplicate",
					"cost": {"input": 1, "output": 2}
				}
			}
		}
	}`

	table, err := ParseModelsDevPriceTable(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseModelsDevPriceTable() error = %v", err)
	}
	price := table.Get("gpt-duplicate")
	if price == nil {
		t.Fatal("expected gpt-duplicate pricing")
	}
	if price.InputPriceMicro != 1_000_000 || price.OutputPriceMicro != 2_000_000 {
		t.Fatalf("duplicate prices = %d/%d, want OpenAI values", price.InputPriceMicro, price.OutputPriceMicro)
	}
}

func TestParseModelsDevPriceTableRejectsCatalogWithoutPricedModels(t *testing.T) {
	_, err := ParseModelsDevPriceTable(strings.NewReader(`{"openai":{"models":{"no-cost":{"id":"no-cost"}}}}`))
	if err == nil {
		t.Fatal("expected error for catalog without priced models")
	}
}
