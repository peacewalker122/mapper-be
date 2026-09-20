package mapper

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var (
	ErrFileStoreUnavailable     = errors.New("file store unavailable")
	ErrSourceAdapterUnavailable = errors.New("source adapter unavailable")
)

// SourceRow is one source record. Number is one-based when supplied by an
// adapter; Values uses source column names as keys.
type SourceRow struct {
	Number int               `json:"number"`
	Values map[string]string `json:"values"`
}

// RowReader streams source records and owns any resources needed by the
// underlying file format.
type RowReader interface {
	Next() (SourceRow, error)
	Close() error
}

// SheetAnalysis describes one logical table in a source file.
type SheetAnalysis struct {
	Name    string      `json:"name"`
	Columns []string    `json:"columns"`
	Samples []SourceRow `json:"samples,omitempty"`
	Rows    int64       `json:"rows,omitempty"`
}

// SourceAnalysis is the format-neutral result of inspecting a source file.
type SourceAnalysis struct {
	File   FileMetadata    `json:"file"`
	Sheets []SheetAnalysis `json:"sheets"`
}

// SourceAdapter analyzes and opens one source format.
type SourceAdapter interface {
	Analyze(context.Context, io.Reader) (SourceAnalysis, error)
	Open(context.Context, io.Reader) (RowReader, error)
}

// Service coordinates schema registration and file-backed mapping operations.
type Service struct {
	registry SchemaRegistry
	files    FileStore
	source   SourceAdapter
	importer Importer
}

// Option configures Service.
type Option func(*Service)

// New constructs a Service with an in-memory schema registry.
func New(options ...Option) *Service {
	service := &Service{registry: NewMemorySchemaRegistry()}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithFileStore(store FileStore) Option {
	return func(service *Service) { service.files = store }
}

func WithSchemaRegistry(registry SchemaRegistry) Option {
	return func(service *Service) {
		if registry != nil {
			service.registry = registry
		}
	}
}

func WithSourceAdapter(adapter SourceAdapter) Option {
	return func(service *Service) { service.source = adapter }
}

func WithImporter[T Importer](importer T) Option {
	return func(service *Service) { service.importer = importer }
}

func (s *Service) RegisterSchema(schema Schema) error {
	if s == nil || s.registry == nil {
		return fmt.Errorf("%w: schema registry unavailable", ErrInvalidSchema)
	}
	return s.registry.Register(schema)
}

func (s *Service) Schema(id uint64) (Schema, error) {
	if s == nil || s.registry == nil {
		return Schema{}, fmt.Errorf("%w: schema registry unavailable", ErrSchemaNotFound)
	}
	return s.registry.Get(id)
}

func (s *Service) Schemas() []Schema {
	if s == nil || s.registry == nil {
		return nil
	}
	return s.registry.List()
}

func (s *Service) AnalyzeFile(ctx context.Context, id FileID) (SourceAnalysis, error) {
	if s == nil || s.files == nil {
		return SourceAnalysis{}, ErrFileStoreUnavailable
	}
	if s.source == nil {
		return SourceAnalysis{}, ErrSourceAdapterUnavailable
	}
	if !id.Valid() {
		return SourceAnalysis{}, fmt.Errorf("%w: empty file id", ErrFileStoreUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	reader, metadata, err := s.files.Open(ctx, id)
	if err != nil {
		return SourceAnalysis{}, fmt.Errorf("open file %q: %w", id, err)
	}
	if reader == nil {
		return SourceAnalysis{}, fmt.Errorf("open file %q: nil reader", id)
	}
	defer reader.Close()

	analysis, err := s.source.Analyze(ctx, reader)
	if err != nil {
		return SourceAnalysis{}, err
	}
	if analysis.File.ID == "" {
		analysis.File = metadata
	}
	return analysis, nil
}
