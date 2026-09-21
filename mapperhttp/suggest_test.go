package mapperhttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peacewalker122/mapper/mapper"
)

func TestSuggestHandlerStatuses(t *testing.T) {
	service := mapper.New()
	if err := service.RegisterSchema(mapper.Schema{
		ID:   7,
		Name: "contacts",
		Fields: []mapper.Field{
			{ID: 10, Name: "email", Type: mapper.TypeString},
		},
	}); err != nil {
		t.Fatal(err)
	}
	configured, err := NewWithConfig(service, Config{Suggester: SuggesterConfig{Provider: "fuzzy"}})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}

	tests := []struct {
		name       string
		handler    http.Handler
		body       string
		status     int
		wantCode   string
		wantTarget uint64
	}{
		{
			name:       "ok",
			handler:    configured,
			body:       `{"schema_id":7,"columns":["email"]}`,
			status:     http.StatusOK,
			wantTarget: 10,
		},
		{
			name:     "invalid schema id",
			handler:  configured,
			body:     `{"schema_id":0,"columns":["email"]}`,
			status:   http.StatusBadRequest,
			wantCode: "invalid_schema_id",
		},
		{
			name:     "schema not found",
			handler:  configured,
			body:     `{"schema_id":99,"columns":["email"]}`,
			status:   http.StatusNotFound,
			wantCode: "schema_not_found",
		},
		{
			name:     "suggester unavailable",
			handler:  New(service),
			body:     `{"schema_id":7,"columns":["email"]}`,
			status:   http.StatusServiceUnavailable,
			wantCode: "suggester_unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, SuggestPath, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			test.handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.status, recorder.Body.String())
			}
			if test.wantTarget != 0 {
				var response mapper.SuggestMappingsResponse
				if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if response.Model != "fuzzy" || len(response.Suggestions) != 1 || response.Suggestions[0].Target != test.wantTarget {
					t.Fatalf("response = %+v", response)
				}
				return
			}
			var response ErrorEnvelope
			if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if response.Error.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", response.Error.Code, test.wantCode)
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	_, err := NewWithConfig(mapper.New(), Config{Suggester: SuggesterConfig{Provider: "jev"}})
	if err == nil || err.Error() != "suggester: provider jev requires api_key" {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
}
