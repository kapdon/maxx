package handler

import (
	"net/http"
	"strings"

	maxxctx "github.com/awsl-project/maxx/internal/context"
	"github.com/awsl-project/maxx/internal/modelavailability"
	"github.com/awsl-project/maxx/internal/repository"
)

// ModelsHandler serves model-list endpoints with a lightweight model list.
type ModelsHandler struct {
	responseModelRepo repository.ResponseModelRepository
	providerRepo      repository.ProviderRepository
	modelMappingRepo  repository.ModelMappingRepository
	modelPriceRepo    repository.ModelPriceRepository
}

// NewModelsHandler creates a new ModelsHandler.
func NewModelsHandler(
	responseModelRepo repository.ResponseModelRepository,
	providerRepo repository.ProviderRepository,
	modelMappingRepo repository.ModelMappingRepository,
	modelPriceRepo repository.ModelPriceRepository,
) *ModelsHandler {
	return &ModelsHandler{
		responseModelRepo: responseModelRepo,
		providerRepo:      providerRepo,
		modelMappingRepo:  modelMappingRepo,
		modelPriceRepo:    modelPriceRepo,
	}
}

// ServeHTTP handles model-list requests.
func (h *ModelsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	tenantID := maxxctx.GetTenantID(r.Context())
	userAgent := r.Header.Get("User-Agent")
	isGeminiModels := isGeminiModelsPath(r.URL.Path)

	var names []string
	var err error
	if isGeminiModels {
		names, err = h.collectModelNames(tenantID)
	} else {
		names, err = h.collectModelNamesForUserAgent(tenantID, userAgent)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if isGeminiModels {
		writeJSON(w, http.StatusOK, buildGeminiModelsResponse(names))
		return
	}

	if strings.HasPrefix(userAgent, "claude-cli") {
		writeJSON(w, http.StatusOK, buildClaudeModelsResponse(names))
		return
	}

	writeJSON(w, http.StatusOK, buildOpenAIModelsResponse(names))
}

func isModelListAPIPath(path string) bool {
	return path == "/v1/models" || isGeminiModelsPath(path)
}

func isGeminiModelsPath(path string) bool {
	return path == "/v1beta/models"
}

func (h *ModelsHandler) collectModelNames(tenantID uint64) ([]string, error) {
	return h.collectModelNamesForUserAgent(tenantID, "")
}

func (h *ModelsHandler) collectModelNamesForUserAgent(tenantID uint64, _ string) ([]string, error) {
	source := modelavailability.Source{
		ResponseModelRepo: h.responseModelRepo,
		ProviderRepo:      h.providerRepo,
		ModelMappingRepo:  h.modelMappingRepo,
	}
	return source.Collect(tenantID, modelavailability.DefaultCollectOptions())
}

func buildOpenAIModelsResponse(names []string) map[string]interface{} {
	data := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		data = append(data, map[string]interface{}{
			"id":       name,
			"object":   "model",
			"created":  0,
			"owned_by": "maxx",
		})
	}

	return map[string]interface{}{
		"object": "list",
		"data":   data,
	}
}

func buildClaudeModelsResponse(names []string) map[string]interface{} {
	data := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		data = append(data, map[string]interface{}{
			"id":           name,
			"display_name": name,
			"type":         "model",
		})
	}

	return map[string]interface{}{
		"data":     data,
		"has_more": false,
	}
}

func buildGeminiModelsResponse(names []string) map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		modelName := name
		if !strings.HasPrefix(modelName, "models/") {
			modelName = "models/" + modelName
		}
		baseModelID := strings.TrimPrefix(modelName, "models/")
		models = append(models, map[string]interface{}{
			"name":                       modelName,
			"baseModelId":                baseModelID,
			"version":                    "",
			"displayName":                baseModelID,
			"description":                "",
			"inputTokenLimit":            0,
			"outputTokenLimit":           0,
			"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"},
		})
	}

	return map[string]interface{}{
		"models": models,
	}
}
