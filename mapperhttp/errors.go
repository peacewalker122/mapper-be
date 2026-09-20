package mapperhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/peacewalker122/mapper/executor"
	"github.com/peacewalker122/mapper/mapper"
	"github.com/peacewalker122/mapper/source"
)

var (
	// ErrInvalidRequest marks malformed or incomplete HTTP input.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrUnsupportedMediaType marks a request body format not supported by an endpoint.
	ErrUnsupportedMediaType = errors.New("unsupported media type")
)

// ErrorDetail is the machine-readable error body returned by this package.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorEnvelope wraps every API error.
type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

// ErrorResponse is an alias kept for callers that name responses explicitly.
type ErrorResponse = ErrorEnvelope

var statusMap = map[error]int{
	ErrInvalidRequest:                  http.StatusBadRequest,
	ErrUnsupportedMediaType:            http.StatusUnsupportedMediaType,
	mapper.ErrInvalidSchema:            http.StatusBadRequest,
	mapper.ErrSchemaExists:             http.StatusConflict,
	mapper.ErrSchemaNotFound:           http.StatusNotFound,
	mapper.ErrFileStoreUnavailable:     http.StatusServiceUnavailable,
	mapper.ErrSourceAdapterUnavailable: http.StatusServiceUnavailable,
	mapper.ErrImporterUnavailable:      http.StatusServiceUnavailable,
	executor.ErrExecutorUnavailable:    http.StatusServiceUnavailable,
	executor.ErrProcessorUnavailable:   http.StatusServiceUnavailable,
	source.ErrNoHeader:                 http.StatusBadRequest,
	os.ErrNotExist:                     http.StatusNotFound,
	context.DeadlineExceeded:           http.StatusRequestTimeout,
	context.Canceled:                   499,
}

// StatusCode maps service and executor errors to HTTP status codes.
func StatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	for cause, status := range statusMap {
		if errors.Is(err, cause) {
			return status
		}
	}
	var mapping *executor.MappingError
	if errors.As(err, &mapping) {
		return http.StatusBadRequest
	}
	var row mapper.RowError
	if errors.As(err, &row) {
		return http.StatusBadRequest
	}
	var rowPointer *mapper.RowError
	if errors.As(err, &rowPointer) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// ErrorCode returns a stable code for an error envelope.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var mapping *executor.MappingError
	if errors.As(err, &mapping) && mapping != nil && mapping.Code != "" {
		return mapping.Code
	}
	var row mapper.RowError
	if errors.As(err, &row) && row.Code != "" {
		return row.Code
	}
	var rowPointer *mapper.RowError
	if errors.As(err, &rowPointer) && rowPointer != nil && rowPointer.Code != "" {
		return rowPointer.Code
	}
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, ErrUnsupportedMediaType):
		return "unsupported_media_type"
	case errors.Is(err, mapper.ErrInvalidSchema):
		return "invalid_schema"
	case errors.Is(err, mapper.ErrSchemaExists):
		return "schema_exists"
	case errors.Is(err, mapper.ErrSchemaNotFound):
		return "schema_not_found"
	case errors.Is(err, mapper.ErrFileStoreUnavailable):
		return "file_store_unavailable"
	case errors.Is(err, mapper.ErrSourceAdapterUnavailable):
		return "source_adapter_unavailable"
	case errors.Is(err, mapper.ErrImporterUnavailable):
		return "importer_unavailable"
	case errors.Is(err, executor.ErrExecutorUnavailable):
		return "executor_unavailable"
	case errors.Is(err, executor.ErrProcessorUnavailable):
		return "processor_unavailable"
	case errors.Is(err, source.ErrNoHeader):
		return "no_header"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "internal_error"
	}
}

// ErrorEnvelopeFor returns the wire representation for err.
func ErrorEnvelopeFor(err error) ErrorEnvelope {
	if err == nil {
		return ErrorEnvelope{}
	}
	return ErrorEnvelope{Error: ErrorDetail{Code: ErrorCode(err), Message: err.Error()}}
}

func writeError(writer http.ResponseWriter, err error) {
	writeErrorStatus(writer, StatusCode(err), ErrorCode(err), errorMessage(err))
}

func writeErrorStatus(writer http.ResponseWriter, status int, code, message string) {
	if status < 400 {
		status = http.StatusInternalServerError
	}
	if code == "" {
		code = "internal_error"
	}
	if message == "" {
		message = http.StatusText(status)
	}
	writeJSON(writer, status, ErrorEnvelope{Error: ErrorDetail{Code: code, Message: message}})
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func requestError(message string) error {
	return errors.Join(ErrInvalidRequest, errors.New(message))
}

func mediaType(request *http.Request) (string, error) {
	if request == nil || request.Header.Get("Content-Type") == "" {
		return "", nil
	}
	value, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	value = strings.ToLower(value)
	return value, err
}

func isJSONMediaType(value string) bool {
	return value == "" || value == "application/json" || strings.HasSuffix(value, "+json")
}

func decodeJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(destination); err != nil {
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

// IsErrorEnvelope verifies the shape without requiring callers to know the
// concrete response type.
func IsErrorEnvelope(data []byte) bool {
	var envelope ErrorEnvelope
	return json.Unmarshal(data, &envelope) == nil && envelope.Error.Code != ""
}
