package mapper

import (
	"context"
	"errors"
	"fmt"
)

var ErrImporterUnavailable = errors.New("importer unavailable")

// ImportRequest describes one synchronous streaming import.
type ImportRequest struct {
	FileID   FileID         `json:"file_id"`
	SchemaID uint64         `json:"schema_id"`
	Sheet    int            `json:"sheet"`
	Mappings []FieldMapping `json:"mappings"`
	FailFast bool           `json:"fail_fast,omitempty"`

	// Processor is kept out of the wire format so an importer can be configured
	// once while requests remain transport-safe.
	Processor ImportProcessor `json:"-"`
}

// RowContext identifies the source row being delivered to a processor.
type RowContext struct {
	FileID   FileID `json:"file_id"`
	SchemaID uint64 `json:"schema_id"`
	Row      int64  `json:"row"`
}

// RowError describes one rejected source row. Errors are retained only up to
// the executor's configured detail limit while counters cover every row.
type RowError struct {
	Row         int64   `json:"row"`
	SourceIndex *int    `json:"source_index,omitempty"`
	TargetID    *uint64 `json:"target_id,omitempty"`
	Code        string  `json:"code"`
	Message     string  `json:"message"`
}

func (e RowError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ImportResult contains full counters and at most 1000 detailed row errors.
type ImportResult struct {
	Processed int64      `json:"processed"`
	Succeeded int64      `json:"succeeded"`
	Failed    int64      `json:"failed"`
	Errors    []RowError `json:"errors,omitempty"`
}

// Importer executes an ImportRequest. Service delegates to this boundary.
type Importer interface {
	Import(context.Context, ImportRequest) (ImportResult, error)
}

func (s *Service) Import(ctx context.Context, request ImportRequest) (ImportResult, error) {
	if s == nil || s.importer == nil {
		return ImportResult{}, ErrImporterUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.importer.Import(ctx, request)
}
