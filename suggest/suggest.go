// Package suggest contains mapping suggestion strategies.
package suggest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/peacewalker122/mapper/mapper"
)

const (
	ProviderFuzzy = "fuzzy"
	ProviderJev   = "jev"

	DefaultJevEndpoint = "https://www.jevai.org/api/v1/decisions"
	defaultJevTimeout  = 5 * time.Second
)

var (
	ErrUnavailable = errors.New("mapping suggester unavailable")
	ErrUpstream    = errors.New("upstream suggestion service failed")
)

// Suggester is the strategy contract accepted by the HTTP adapter.
type Suggester = mapper.Suggester

// Result adds strategy metadata to suggestions while keeping Suggester small.
type Result struct {
	Suggestions []mapper.MappingSuggestion
	Model       string
}

// ResultSuggester is an optional extension used by HTTP responses to report
// fallback from Jev to fuzzy.
type ResultSuggester interface {
	SuggestResult(context.Context, mapper.Schema, []string, []mapper.ColumnSample) (Result, error)
}

// Suggest invokes strategy metadata when available.
func Suggest(ctx context.Context, suggester Suggester, schema mapper.Schema, columns []string, samples []mapper.ColumnSample) (Result, error) {
	if suggester == nil {
		return Result{}, ErrUnavailable
	}
	if resultSuggester, ok := suggester.(ResultSuggester); ok {
		return resultSuggester.SuggestResult(ctx, schema, columns, samples)
	}
	suggestions, err := suggester.Suggest(ctx, schema, columns, samples)
	return Result{Suggestions: suggestions, Model: ProviderFuzzy}, err
}

// Config selects suggestion strategy and its server-side dependencies.
type Config struct {
	Provider   string
	APIKey     string
	Endpoint   string
	Timeout    time.Duration
	HTTPClient *http.Client
	Model      string
}

// Validate checks strategy configuration before handler construction.
func (config Config) Validate() error {
	provider := strings.ToLower(strings.TrimSpace(config.Provider))
	switch provider {
	case "", ProviderFuzzy:
		return nil
	case ProviderJev:
		if strings.TrimSpace(config.APIKey) == "" {
			return fmt.Errorf("suggester: provider jev requires api_key")
		}
		return nil
	default:
		return fmt.Errorf("suggester: unsupported provider %q", config.Provider)
	}
}

// New constructs a configured suggester. Empty config selects fuzzy.
func New(config ...Config) (Suggester, error) {
	selected := Config{}
	if len(config) > 0 {
		selected = config[0]
	}
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(selected.Provider)) {
	case "", ProviderFuzzy:
		return FuzzySuggester{}, nil
	case ProviderJev:
		return &JevSuggester{
			APIKey:     selected.APIKey,
			Endpoint:   selected.Endpoint,
			Timeout:    selected.Timeout,
			HTTPClient: selected.HTTPClient,
		}, nil
	default:
		return nil, fmt.Errorf("suggester: unsupported provider %q", selected.Provider)
	}
}

// NewWithConfig is an explicit constructor alias.
func NewWithConfig(config Config) (Suggester, error) { return New(config) }

// FuzzySuggester matches normalized source names to target field names.
type FuzzySuggester struct{}

var _ Suggester = FuzzySuggester{}
var _ ResultSuggester = FuzzySuggester{}

func NewFuzzySuggester() FuzzySuggester { return FuzzySuggester{} }

func (FuzzySuggester) Suggest(ctx context.Context, schema mapper.Schema, columns []string, samples []mapper.ColumnSample) ([]mapper.MappingSuggestion, error) {
	result, err := (FuzzySuggester{}).SuggestResult(ctx, schema, columns, samples)
	return result.Suggestions, err
}

func (FuzzySuggester) SuggestResult(ctx context.Context, schema mapper.Schema, columns []string, samples []mapper.ColumnSample) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	suggestions := make([]mapper.MappingSuggestion, 0, len(columns))
	for sourceIndex, column := range columns {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		sourceName := NormalizeName(column)
		if sourceName == "" {
			continue
		}
		bestIndex := -1
		bestDistance := 0
		bestConfidence := -1.0
		for targetIndex, field := range schema.Fields {
			targetName := NormalizeName(field.Name)
			if targetName == "" || field.ID == 0 {
				continue
			}
			distance := LevenshteinDistance(sourceName, targetName)
			confidence := fuzzyConfidence(distance, sourceName, targetName)
			if bestIndex < 0 || confidence > bestConfidence {
				bestIndex = targetIndex
				bestDistance = distance
				bestConfidence = confidence
			}
		}
		if bestIndex < 0 {
			continue
		}
		field := schema.Fields[bestIndex]
		suggestions = append(suggestions, mapper.MappingSuggestion{
			Source:     sourceIndex,
			Target:     field.ID,
			Confidence: bestConfidence,
			Reason:     fmt.Sprintf("fuzzy match to %q (distance %d)", field.Name, bestDistance),
		})
	}
	return Result{Suggestions: suggestions, Model: ProviderFuzzy}, nil
}

// NormalizeName applies the name normalization shared by fuzzy matching.
func NormalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var normalized strings.Builder
	underscore := false
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			normalized.WriteRune(character)
			underscore = false
			continue
		}
		if normalized.Len() > 0 && !underscore {
			normalized.WriteByte('_')
			underscore = true
		}
	}
	return strings.Trim(normalized.String(), "_")
}

// LevenshteinDistance returns edit distance over Unicode code points.
func LevenshteinDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}
	if len(leftRunes) > len(rightRunes) {
		leftRunes, rightRunes = rightRunes, leftRunes
	}
	previous := make([]int, len(leftRunes)+1)
	current := make([]int, len(leftRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for rightIndex, rightRune := range rightRunes {
		current[0] = rightIndex + 1
		for leftIndex, leftRune := range leftRunes {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[leftIndex+1] = min3(
				current[leftIndex]+1,
				previous[leftIndex+1]+1,
				previous[leftIndex]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(leftRunes)]
}

func fuzzyConfidence(distance int, left, right string) float64 {
	maxLength := len([]rune(left))
	if rightLength := len([]rune(right)); rightLength > maxLength {
		maxLength = rightLength
	}
	if maxLength == 0 {
		return 0
	}
	confidence := 1 - float64(distance)/float64(maxLength)
	if confidence < 0 {
		return 0
	}
	return confidence
}

func min3(left, middle, right int) int {
	if left < middle {
		if left < right {
			return left
		}
		return right
	}
	if middle < right {
		return middle
	}
	return right
}

// JevState is the per-column state sent to Jev.
type JevState struct {
	Column  string `json:"column"`
	Samples []any  `json:"samples"`
}

// JevChoice is one target field offered to Jev.
type JevChoice struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// JevQuestion asks Jev to choose one target field.
type JevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

// JevRequest is the server-side Jev request. API keys never enter this body.
type JevRequest struct {
	Model     string                 `json:"model,omitempty"`
	State     JevState               `json:"state"`
	Questions map[string]JevQuestion `json:"questions"`
}

// JevSuggester asks Jev for choices and falls back to fuzzy matching whenever
// the upstream call fails, times out, or returns an unusable response.
type JevSuggester struct {
	APIKey     string
	Endpoint   string
	Timeout    time.Duration
	HTTPClient *http.Client
	Model      string
	Fallback   Suggester
}

var _ Suggester = (*JevSuggester)(nil)
var _ ResultSuggester = (*JevSuggester)(nil)

func NewJevSuggester(config Config) (*JevSuggester, error) {
	config.Provider = ProviderJev
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &JevSuggester{
		APIKey:     config.APIKey,
		Endpoint:   config.Endpoint,
		Timeout:    config.Timeout,
		HTTPClient: config.HTTPClient,
		Model:      config.Model,
	}, nil
}

func (s *JevSuggester) Suggest(ctx context.Context, schema mapper.Schema, columns []string, samples []mapper.ColumnSample) ([]mapper.MappingSuggestion, error) {
	result, err := s.SuggestResult(ctx, schema, columns, samples)
	return result.Suggestions, err
}

func (s *JevSuggester) SuggestResult(ctx context.Context, schema mapper.Schema, columns []string, samples []mapper.ColumnSample) (Result, error) {
	fallback := s.fuzzyFallback()
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || strings.TrimSpace(s.APIKey) == "" {
		return fallback.SuggestResult(ctx, schema, columns, samples)
	}

	suggestions := make([]mapper.MappingSuggestion, 0, len(columns))
	for sourceIndex, column := range columns {
		target, confidence, err := s.suggestColumn(ctx, schema, column, samples)
		if err != nil {
			return fallback.SuggestResult(ctx, schema, columns, samples)
		}
		suggestions = append(suggestions, mapper.MappingSuggestion{
			Source:     sourceIndex,
			Target:     target,
			Confidence: confidence,
			Reason:     "jev choice over target fields",
		})
	}
	return Result{Suggestions: suggestions, Model: ProviderJev}, nil
}

func (s *JevSuggester) fuzzyFallback() FuzzySuggester {
	if s != nil && s.Fallback != nil {
		if fuzzy, ok := s.Fallback.(FuzzySuggester); ok {
			return fuzzy
		}
	}
	return FuzzySuggester{}
}

func (s *JevSuggester) suggestColumn(ctx context.Context, schema mapper.Schema, column string, samples []mapper.ColumnSample) (uint64, float64, error) {
	state := JevState{Column: column, Samples: sampleValues(column, samples)}
	criteria := make(map[string]string, len(schema.Fields))
	for _, field := range schema.Fields {
		if field.ID != 0 {
			criteria[strconv.FormatUint(field.ID, 10)] = field.Name
		}
	}
	questions := map[string]JevQuestion{
		"target": {
			Type:         "choice",
			Instructions: "Choose the target field for this source column.",
			Criteria:     criteria,
		},
	}
	model := ""
	if s != nil {
		model = strings.TrimSpace(s.Model)
	}
	payload, err := json.Marshal(JevRequest{Model: model, State: state, Questions: questions})
	if err != nil {
		return 0, 0, fmt.Errorf("%w: encode request: %v", ErrUpstream, err)
	}

	endpoint := strings.TrimSpace(s.Endpoint)
	if endpoint == "" {
		endpoint = DefaultJevEndpoint
	}
	callContext := ctx
	cancel := func() {}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultJevTimeout
	}
	callContext, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callContext, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: create request: %v", ErrUpstream, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.APIKey)

	client := s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: request: %v", ErrUpstream, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: read response: %v", ErrUpstream, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, 0, fmt.Errorf("%w: status %d", ErrUpstream, response.StatusCode)
	}
	return parseJevResponse(body, schema)
}

func sampleValues(column string, samples []mapper.ColumnSample) []any {
	values := make([]any, 0, len(samples))
	for _, sample := range samples {
		if sample.Values == nil {
			continue
		}
		if value, ok := sample.Values[column]; ok {
			values = append(values, value)
		}
	}
	return values
}

func parseJevResponse(data []byte, schema mapper.Schema) (uint64, float64, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return 0, 0, fmt.Errorf("%w: decode response: %v", ErrUpstream, err)
	}
	if code, ok := numberValue(payload["code"]); ok && code != 0 {
		return 0, 0, fmt.Errorf("%w: Jev response code %v", ErrUpstream, code)
	}
	if dataValue, ok := payload["data"].(map[string]any); ok {
		if answers, ok := dataValue["answers"].(map[string]any); ok {
			if answer, ok := answers["target"].(map[string]any); ok {
				if target, confidence, found := parseJevAnswer(answer, schema); found {
					return target, confidence, nil
				}
			}
		}
	}
	if probabilities, ok := firstValue(payload, "probabilities", "probs"); ok {
		if target, confidence, found := bestProbability(probabilities, schema); found {
			return target, confidence, nil
		}
	}
	if choices, ok := firstValue(payload, "choices"); ok {
		if target, confidence, found := bestProbability(choices, schema); found {
			return target, confidence, nil
		}
	}
	for _, nestedKey := range []string{"result", "data", "choice"} {
		if nested, ok := payload[nestedKey].(map[string]any); ok {
			if target, confidence, found := parseJevAnswer(nested, schema); found {
				return target, confidence, nil
			}
		}
	}
	if target, ok := targetFromMap(payload, schema); ok {
		return target, confidenceFromMap(payload), nil
	}
	return 0, 0, fmt.Errorf("%w: response contains no target choice", ErrUpstream)
}

func parseJevAnswer(answer map[string]any, schema mapper.Schema) (uint64, float64, bool) {
	choice, choiceFound := firstValue(answer, "choice", "target", "target_id", "field_id", "id", "name")
	probabilities, probabilitiesFound := firstValue(answer, "probabilities", "probs")
	if choiceFound {
		if target, ok := targetFromValue(choice, schema); ok {
			if probabilitiesFound {
				if confidence, found := probabilityForTarget(probabilities, target, schema); found {
					return target, confidence, true
				}
			}
			return target, confidenceFromMap(answer), true
		}
	}
	if probabilitiesFound {
		if target, confidence, found := bestProbability(probabilities, schema); found {
			return target, confidence, true
		}
	}
	return 0, 0, false
}

func probabilityForTarget(value any, target uint64, schema mapper.Schema) (float64, bool) {
	values, ok := value.(map[string]any)
	if !ok {
		return 0, false
	}
	for choice, probability := range values {
		resolved, found := targetFromValue(choice, schema)
		if !found || resolved != target {
			continue
		}
		if nested, ok := probability.(map[string]any); ok {
			probability, _ = firstValue(nested, "probability", "confidence", "score")
		}
		if number, ok := numberValue(probability); ok {
			return clampConfidence(number), true
		}
	}
	return 0, false
}

func bestProbability(value any, schema mapper.Schema) (uint64, float64, bool) {
	bestTarget := uint64(0)
	bestConfidence := -1.0
	consider := func(targetValue, probabilityValue any) {
		target, ok := targetFromValue(targetValue, schema)
		if !ok {
			return
		}
		probability, ok := numberValue(probabilityValue)
		if !ok {
			return
		}
		probability = clampConfidence(probability)
		if probability > bestConfidence {
			bestTarget = target
			bestConfidence = probability
		}
	}
	switch values := value.(type) {
	case map[string]any:
		for target, probability := range values {
			if nested, ok := probability.(map[string]any); ok {
				if nestedProbability, found := firstValue(nested, "probability", "confidence", "score"); found {
					consider(target, nestedProbability)
				}
				continue
			}
			consider(target, probability)
		}
	case []any:
		for index, item := range values {
			if nested, ok := item.(map[string]any); ok {
				targetValue, targetFound := firstValue(nested, "target", "target_id", "field_id", "id", "choice", "name")
				probabilityValue, probabilityFound := firstValue(nested, "probability", "confidence", "score")
				if !targetFound && index < len(schema.Fields) {
					targetValue = schema.Fields[index].ID
				}
				if probabilityFound {
					consider(targetValue, probabilityValue)
				}
				continue
			}
			if index < len(schema.Fields) {
				consider(schema.Fields[index].ID, item)
			}
		}
	}
	return bestTarget, bestConfidence, bestConfidence >= 0
}

func targetFromMap(value map[string]any, schema mapper.Schema) (uint64, bool) {
	for _, key := range []string{"target", "target_id", "field_id", "id", "choice", "name", "value"} {
		if target, ok := value[key]; ok {
			if resolved, found := targetFromValue(target, schema); found {
				return resolved, true
			}
		}
	}
	return 0, false
}

func targetFromValue(value any, schema mapper.Schema) (uint64, bool) {
	switch target := value.(type) {
	case json.Number:
		id, err := strconv.ParseUint(string(target), 10, 64)
		return schemaTarget(id, schema, err == nil)
	case float64:
		if target < 0 || target != float64(uint64(target)) {
			return 0, false
		}
		return schemaTarget(uint64(target), schema, true)
	case string:
		if id, err := strconv.ParseUint(strings.TrimSpace(target), 10, 64); err == nil {
			if resolved, ok := schemaTarget(id, schema, true); ok {
				return resolved, true
			}
		}
		normalized := NormalizeName(target)
		for _, field := range schema.Fields {
			if NormalizeName(field.Name) == normalized && field.ID != 0 {
				return field.ID, true
			}
		}
	case map[string]any:
		return targetFromMap(target, schema)
	}
	return 0, false
}

func schemaTarget(id uint64, schema mapper.Schema, valid bool) (uint64, bool) {
	if !valid || id == 0 {
		return 0, false
	}
	for _, field := range schema.Fields {
		if field.ID == id {
			return id, true
		}
	}
	return 0, false
}

func confidenceFromMap(value map[string]any) float64 {
	for _, key := range []string{"confidence", "probability", "score"} {
		if confidence, ok := value[key]; ok {
			if number, ok := numberValue(confidence); ok {
				return clampConfidence(number)
			}
		}
	}
	return 0
}

func numberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case float64:
		return number, true
	case int:
		return float64(number), true
	default:
		return 0, false
	}
}

func firstValue(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
