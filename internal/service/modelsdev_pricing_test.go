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
					}
				}
			}
		}`)
	}))
	defer server.Close()

	svc := newModelPriceOnlyAdminService(repo)
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background())
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() error = %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("updated prices count = %d, want 1", len(prices))
	}
	if repo.hasModel("old-model") {
		t.Fatal("old-model should have been wiped before models.dev prices were inserted")
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

	svc := newModelPriceOnlyAdminService(repo)
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	prices, err := svc.UpdateModelPricesFromModelsDev(context.Background())
	if err != nil {
		t.Fatalf("UpdateModelPricesFromModelsDev() fallback error = %v", err)
	}
	if len(prices) != len(pricing.DefaultPriceTable().All()) {
		t.Fatalf("fallback prices count = %d, want %d", len(prices), len(pricing.DefaultPriceTable().All()))
	}
	if repo.hasModel("old-model") {
		t.Fatal("old-model should have been wiped before fallback defaults were inserted")
	}
	if !repo.hasModel("claude-sonnet-4-5") {
		t.Fatal("expected hardcoded default price claude-sonnet-4-5 after fallback")
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

	svc := newModelPriceOnlyAdminService(repo)
	svc.SetModelsDevPricingSource(server.URL, server.Client())

	if _, err := svc.UpdateModelPricesFromModelsDev(context.Background()); err != nil {
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

func newModelPriceOnlyAdminService(repo *adminServiceModelPriceRepo) *AdminService {
	return NewAdminService(
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
		nil,
		repo,
		"",
		nil,
		nil,
		nil,
	)
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
