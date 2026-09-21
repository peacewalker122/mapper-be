package mapper

import "context"

// ColumnSample is one sampled source row supplied to a mapping suggester.
type ColumnSample struct {
	Number int            `json:"number"`
	Values map[string]any `json:"values"`
}

// Suggester proposes mappings without changing schemas or import state.
type Suggester interface {
	Suggest(context.Context, Schema, []string, []ColumnSample) ([]MappingSuggestion, error)
}

// SuggestMappingsOptions controls filtering of generated suggestions.
type SuggestMappingsOptions struct {
	MinConfidence float64 `json:"min_confidence,omitempty"`
	Limit         int     `json:"limit,omitempty"`
}

// SuggestMappingsRequest is the wire request for mapping suggestions.
type SuggestMappingsRequest struct {
	SchemaID uint64                 `json:"schema_id"`
	Columns  []string               `json:"columns"`
	Samples  []ColumnSample         `json:"samples,omitempty"`
	Options  SuggestMappingsOptions `json:"options,omitempty"`
}

// MappingSuggestion is a proposed source-column to target-field mapping.
// Suggestions are advisory; callers must explicitly confirm them before use.
type MappingSuggestion struct {
	Source     int     `json:"source"`
	Target     uint64  `json:"target"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// Suggestion is a short alias for MappingSuggestion.
type Suggestion = MappingSuggestion

// SuggestMappingsResponse is the wire response for mapping suggestions.
type SuggestMappingsResponse struct {
	Suggestions []MappingSuggestion `json:"suggestions"`
	Model       string              `json:"model"`
}
