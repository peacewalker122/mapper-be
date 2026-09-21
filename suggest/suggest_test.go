package suggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/peacewalker122/mapper/mapper"
)

func TestFuzzySuggesterNormalizesNames(t *testing.T) {
	schema := mapper.Schema{Fields: []mapper.Field{
		{ID: 10, Name: "first_name"},
		{ID: 20, Name: "email"},
	}}

	got, err := (FuzzySuggester{}).Suggest(context.Background(), schema, []string{" First Name ", "EMAIL"}, nil)
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Suggest() returned %d suggestions, want 2", len(got))
	}
	if got[0].Source != 0 || got[0].Target != 10 || got[0].Confidence != 1 {
		t.Fatalf("first suggestion = %+v", got[0])
	}
	if got[1].Source != 1 || got[1].Target != 20 || got[1].Confidence != 1 {
		t.Fatalf("second suggestion = %+v", got[1])
	}
}

func TestJevSuggesterUsesProbabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer server-secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("X-API-Key") != "" {
			t.Errorf("Jev API key leaked through X-API-Key header")
		}
		var payload JevRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.State.Column != "email" || len(payload.State.Samples) != 1 {
			t.Errorf("state = %+v", payload.State)
		}
		question, ok := payload.Questions["target"]
		if !ok || question.Type != "choice" || question.Criteria["10"] != "name" || question.Criteria["20"] != "email" {
			t.Errorf("questions = %+v", payload.Questions)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"answers":{"target":{"choice":"20","probabilities":{"10":0.2,"20":0.8}}}}}`))
	}))
	defer server.Close()

	suggester := &JevSuggester{
		APIKey:     "server-secret",
		Endpoint:   server.URL,
		HTTPClient: server.Client(),
		Timeout:    time.Second,
	}
	schema := mapper.Schema{Fields: []mapper.Field{
		{ID: 10, Name: "name"},
		{ID: 20, Name: "email"},
	}}
	result, err := suggester.SuggestResult(context.Background(), schema, []string{"email"}, []mapper.ColumnSample{
		{Number: 1, Values: map[string]any{"email": "ada@example.com"}},
	})
	if err != nil {
		t.Fatalf("SuggestResult() error = %v", err)
	}
	if result.Model != ProviderJev || len(result.Suggestions) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Suggestions[0].Target != 20 || result.Suggestions[0].Confidence != 0.8 {
		t.Fatalf("suggestion = %+v", result.Suggestions[0])
	}
}

func TestJevSuggesterFallsBackToFuzzyOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	suggester := &JevSuggester{
		APIKey:     "server-secret",
		Endpoint:   server.URL,
		HTTPClient: server.Client(),
		Timeout:    time.Second,
	}
	schema := mapper.Schema{Fields: []mapper.Field{{ID: 20, Name: "email"}}}
	result, err := suggester.SuggestResult(context.Background(), schema, []string{"email"}, nil)
	if err != nil {
		t.Fatalf("SuggestResult() error = %v", err)
	}
	if result.Model != ProviderFuzzy || len(result.Suggestions) != 1 || result.Suggestions[0].Target != 20 {
		t.Fatalf("fallback result = %+v", result)
	}
}
