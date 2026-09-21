package mapperhttp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/peacewalker122/mapper/mapper"
	suggestpkg "github.com/peacewalker122/mapper/suggest"
)

const SuggestPath = "/mappings/suggest"

// Suggester is the strategy contract accepted by Handler.
type Suggester = suggestpkg.Suggester

// SuggesterConfig configures fuzzy or Jev suggestions.
type SuggesterConfig = suggestpkg.Config

// Config configures HTTP suggestion handling.
type Config struct {
	Suggester SuggesterConfig
}

// Validate checks all suggestion dependencies before serving requests.
func (config Config) Validate() error { return config.Suggester.Validate() }

// WithSuggester installs an already-constructed suggestion strategy.
func WithSuggester(value Suggester) Option {
	return func(handler *Handler) { handler.suggester = value }
}

// NewWithConfig constructs Handler with a validated suggestion strategy.
func NewWithConfig(service *mapper.Service, config Config, components ...any) (*Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	strategy, err := suggestpkg.New(config.Suggester)
	if err != nil {
		return nil, err
	}
	components = append(components, WithSuggester(strategy))
	return New(service, components...), nil
}

func (handler *Handler) handleSuggest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if handler == nil || handler.service == nil {
		writeErrorStatus(writer, http.StatusServiceUnavailable, "service_unavailable", "mapper service is unavailable")
		return
	}
	if handler.suggester == nil {
		writeErrorStatus(writer, http.StatusServiceUnavailable, "suggester_unavailable", "mapping suggester is unavailable")
		return
	}

	contentType, err := mediaType(request)
	if err != nil {
		writeErrorStatus(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error())
		return
	}
	if !isJSONMediaType(contentType) {
		writeErrorStatus(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "request must use application/json")
		return
	}

	var payload mapper.SuggestMappingsRequest
	if err := decodeSuggestRequest(request.Body, &payload); err != nil {
		writeError(writer, err)
		return
	}
	if payload.SchemaID == 0 {
		writeErrorStatus(writer, http.StatusBadRequest, "invalid_schema_id", "schema_id must be a positive integer")
		return
	}
	if len(payload.Columns) == 0 {
		writeErrorStatus(writer, http.StatusBadRequest, "invalid_columns", "columns must contain at least one column name")
		return
	}
	for _, column := range payload.Columns {
		if strings.TrimSpace(column) == "" {
			writeErrorStatus(writer, http.StatusBadRequest, "invalid_columns", "columns must contain only non-empty column names")
			return
		}
	}

	schema, err := handler.service.Schema(payload.SchemaID)
	if err != nil {
		writeError(writer, err)
		return
	}
	result, err := suggestpkg.Suggest(request.Context(), handler.suggester, schema, payload.Columns, payload.Samples)
	if err != nil {
		if errors.Is(err, suggestpkg.ErrUnavailable) {
			writeErrorStatus(writer, http.StatusServiceUnavailable, "suggester_unavailable", "mapping suggester is unavailable")
			return
		}
		writeErrorStatus(writer, http.StatusBadGateway, "upstream_error", "upstream suggestion service failed")
		return
	}

	suggestions := filterSuggestions(result.Suggestions, payload.Options)
	model := strings.TrimSpace(result.Model)
	if model == "" {
		model = suggestpkg.ProviderFuzzy
	}
	writeJSON(writer, http.StatusOK, mapper.SuggestMappingsResponse{Suggestions: suggestions, Model: model})
}

func decodeSuggestRequest(reader io.Reader, payload *mapper.SuggestMappingsRequest) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return requestError("invalid JSON: " + err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return requestError("request contains multiple JSON values")
		}
		return requestError("invalid JSON: " + err.Error())
	}
	return nil
}

func filterSuggestions(suggestions []mapper.MappingSuggestion, options mapper.SuggestMappingsOptions) []mapper.MappingSuggestion {
	filtered := suggestions
	if options.MinConfidence > 0 {
		filtered = make([]mapper.MappingSuggestion, 0, len(suggestions))
		for _, suggestion := range suggestions {
			if suggestion.Confidence >= options.MinConfidence {
				filtered = append(filtered, suggestion)
			}
		}
	}
	if options.Limit > 0 && len(filtered) > options.Limit {
		filtered = filtered[:options.Limit]
	}
	return filtered
}
