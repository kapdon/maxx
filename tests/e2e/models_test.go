package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetModels(t *testing.T) {
	env := NewTestEnv(t)

	resp := env.UnauthGet("/v1/models")
	AssertStatus(t, resp, http.StatusOK)

	var result map[string]any
	DecodeJSON(t, resp, &result)

	// OpenAI-style response should have "object" and "data" fields
	if result["object"] != "list" {
		t.Fatalf("Expected object 'list', got %v", result["object"])
	}

	data, ok := result["data"].([]any)
	if !ok {
		t.Fatal("Expected 'data' to be an array")
	}

	// Verify each model entry has expected fields
	for i, item := range data {
		model, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("Expected model entry %d to be an object", i)
		}
		if model["id"] == nil || model["id"] == "" {
			t.Fatalf("Expected model entry %d to have non-empty 'id'", i)
		}
		if model["object"] != "model" {
			t.Fatalf("Expected model entry %d object to be 'model', got %v", i, model["object"])
		}
	}
}

func TestGetModels_ResponseFormat(t *testing.T) {
	env := NewTestEnv(t)
	createProviderWithSupportModels(t, env, []string{"gpt-e2e-available"})

	resp := env.UnauthGet("/v1/models")
	AssertStatus(t, resp, http.StatusOK)

	var result map[string]any
	DecodeJSON(t, resp, &result)

	// Verify OpenAI-compatible top-level fields
	if result["object"] != "list" {
		t.Fatalf("Expected top-level 'object' to be 'list', got %v", result["object"])
	}

	data, ok := result["data"].([]any)
	if !ok {
		t.Fatal("Expected 'data' to be an array")
	}

	if !containsOpenAIModelData(data, "gpt-e2e-available") {
		t.Fatal("expected model list to include provider-supported model")
	}

	// Verify OpenAI-compatible fields on each model entry
	model, ok := data[0].(map[string]any)
	if !ok {
		t.Fatal("Expected first model entry to be an object")
	}

	requiredFields := []string{"id", "object", "created", "owned_by"}
	for _, field := range requiredFields {
		if _, exists := model[field]; !exists {
			t.Fatalf("Expected model entry to contain OpenAI-compatible field '%s'", field)
		}
	}

	if model["object"] != "model" {
		t.Fatalf("Expected model entry 'object' to be 'model', got %v", model["object"])
	}
}

func TestModelPricesDoNotFeedAvailableModelsForMappingOptions(t *testing.T) {
	env := NewTestEnv(t)

	for _, price := range []map[string]any{
		{
			"modelId":          "gpt-live-mapping-option",
			"inputPriceMicro":  1000,
			"outputPriceMicro": 2000,
		},
		{
			"modelId":          "claude-live-mapping-option",
			"inputPriceMicro":  3000,
			"outputPriceMicro": 4000,
		},
	} {
		resp := env.AdminPost("/api/admin/model-prices", price)
		AssertStatus(t, resp, http.StatusCreated)
		resp.Body.Close()
	}

	openAIIDs := fetchModelIDsForUserAgent(t, env, "codex_cli_rs/0.99.0")
	if containsModelID(openAIIDs, "gpt-live-mapping-option") || containsModelID(openAIIDs, "claude-live-mapping-option") {
		t.Fatalf("did not expect model prices to advertise availability")
	}

	claudeIDs := fetchModelIDsForUserAgent(t, env, "claude-cli/2.1.17")
	if containsModelID(claudeIDs, "gpt-live-mapping-option") || containsModelID(claudeIDs, "claude-live-mapping-option") {
		t.Fatalf("did not expect model prices to advertise availability in Claude format")
	}
}

func TestProviderSupportModelsFeedAvailableModels(t *testing.T) {
	env := NewTestEnv(t)
	createProviderWithSupportModels(t, env, []string{"gpt-live-mapping-option", "claude-live-mapping-option", "*"})

	ids := fetchModelIDsForUserAgent(t, env, "codex_cli_rs/0.99.0")
	if !containsModelID(ids, "gpt-live-mapping-option") {
		t.Fatalf("expected provider-supported model in available model list")
	}
	if !containsModelID(ids, "claude-live-mapping-option") {
		t.Fatalf("expected all concrete provider-supported models in available model list")
	}
	if containsModelID(ids, "*") {
		t.Fatalf("did not expect wildcard support model in available model list")
	}
}

func TestProviderModelsEndpointFeedsAvailableModels(t *testing.T) {
	env := NewTestEnv(t)
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-provider-endpoint-option"}]}`))
	}))
	t.Cleanup(modelServer.Close)
	createModelsProvider(t, env, modelServer.URL, nil)

	ids := fetchModelIDsForUserAgent(t, env, "codex_cli_rs/0.99.0")
	if !containsModelID(ids, "gpt-provider-endpoint-option") {
		t.Fatalf("expected provider /v1/models endpoint model in available model list")
	}
}

func createProviderWithSupportModels(t *testing.T, env *TestEnv, supportModels []string) {
	t.Helper()
	modelServer := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(modelServer.Close)
	createModelsProvider(t, env, modelServer.URL, supportModels)
}

func createModelsProvider(t *testing.T, env *TestEnv, baseURL string, supportModels []string) {
	t.Helper()
	provider := map[string]any{
		"name": "models-provider",
		"type": "custom",
		"config": map[string]any{
			"custom": map[string]any{
				"baseURL": baseURL,
				"apiKey":  "sk-test-key",
			},
		},
		"supportedClientTypes": []string{"codex"},
		"supportModels":        supportModels,
	}
	resp := env.AdminPost("/api/admin/providers", provider)
	AssertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()
}

func containsOpenAIModelData(data []any, want string) bool {
	for _, item := range data {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if model["id"] == want {
			return true
		}
	}
	return false
}

func fetchModelIDsForUserAgent(t *testing.T, env *TestEnv, userAgent string) []string {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, env.URL("/v1/models"), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	DecodeJSON(t, resp, &result)

	ids := make([]string, 0, len(result.Data))
	for _, item := range result.Data {
		ids = append(ids, item.ID)
	}
	return ids
}

func containsModelID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
