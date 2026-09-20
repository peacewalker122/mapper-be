package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/peacewalker122/mapper/mapper"
)

const MaxErrors = 1000

var (
	ErrExecutorUnavailable  = errors.New("executor unavailable")
	ErrProcessorUnavailable = errors.New("import processor unavailable")
)

// Options supplies the runtime dependencies needed for streaming imports.
type Options struct {
	Registry      mapper.SchemaRegistry
	Files         mapper.FileStore
	Source        mapper.SourceAdapter
	Processor     mapper.ImportProcessor
	SourceColumns []string
}

// Option configures an Executor.
type Option func(*Executor)

func WithSchemaRegistry(registry mapper.SchemaRegistry) Option {
	return func(executor *Executor) { executor.Registry = registry }
}

func WithRegistry(registry mapper.SchemaRegistry) Option {
	return WithSchemaRegistry(registry)
}

func WithFileStore(files mapper.FileStore) Option {
	return func(executor *Executor) { executor.Files = files }
}

func WithSourceAdapter(source mapper.SourceAdapter) Option {
	return func(executor *Executor) { executor.Source = source }
}

func WithImportProcessor(processor mapper.ImportProcessor) Option {
	return func(executor *Executor) { executor.Processor = processor }
}

func WithProcessor(processor mapper.ImportProcessor) Option {
	return WithImportProcessor(processor)
}

func WithSourceColumns(columns []string) Option {
	return func(executor *Executor) { executor.SourceColumns = append([]string(nil), columns...) }
}

// Executor implements mapper.Importer.
type Executor struct {
	Registry      mapper.SchemaRegistry
	Files         mapper.FileStore
	Source        mapper.SourceAdapter
	Processor     mapper.ImportProcessor
	SourceColumns []string
}

// ImportExecutor is a descriptive alias for Executor.
type ImportExecutor = Executor

func New(options ...Option) *Executor {
	executor := &Executor{}
	for _, option := range options {
		if option != nil {
			option(executor)
		}
	}
	return executor
}

func NewWithOptions(options Options) *Executor {
	return &Executor{
		Registry:      options.Registry,
		Files:         options.Files,
		Source:        options.Source,
		Processor:     options.Processor,
		SourceColumns: append([]string(nil), options.SourceColumns...),
	}
}

func NewExecutor(registry mapper.SchemaRegistry, files mapper.FileStore, source mapper.SourceAdapter, processor mapper.ImportProcessor) *Executor {
	return New(
		WithSchemaRegistry(registry),
		WithFileStore(files),
		WithSourceAdapter(source),
		WithImportProcessor(processor),
	)
}

func NewImportExecutor(registry mapper.SchemaRegistry, files mapper.FileStore, source mapper.SourceAdapter, processor mapper.ImportProcessor) *Executor {
	return NewExecutor(registry, files, source, processor)
}

// Import streams rows through one compiled plan and processor.
func (e *Executor) Import(ctx context.Context, request mapper.ImportRequest) (mapper.ImportResult, error) {
	var result mapper.ImportResult
	if e == nil || e.Registry == nil || e.Files == nil || e.Source == nil {
		return result, ErrExecutorUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !request.FileID.Valid() {
		return result, mappingError(CodeInvalidMapping, "file ID is empty", nil, nil)
	}
	if request.SchemaID == 0 {
		return result, mappingError(CodeInvalidMapping, "schema ID is zero", nil, nil)
	}
	schema, err := e.Registry.Get(request.SchemaID)
	if err != nil {
		return result, fmt.Errorf("load schema %d: %w", request.SchemaID, err)
	}

	processor := e.Processor
	if request.Processor != nil {
		processor = request.Processor
	}
	if processor == nil {
		return result, ErrProcessorUnavailable
	}

	reader, _, err := e.Files.Open(ctx, request.FileID)
	if err != nil {
		return result, fmt.Errorf("open file %q: %w", request.FileID, err)
	}
	if reader == nil {
		return result, fmt.Errorf("open file %q: nil reader", request.FileID)
	}
	defer reader.Close()

	if request.Sheet < 0 {
		return result, mappingError(CodeInvalidMapping, "sheet index is negative", nil, nil)
	}
	columns, err := e.columnsFromSource(ctx, reader, request.Sheet)
	if err != nil {
		return result, err
	}
	rowReader, err := openRows(ctx, e.Source, reader, request.Sheet)
	if err != nil {
		return result, fmt.Errorf("open source rows: %w", err)
	}
	if rowReader == nil {
		return result, fmt.Errorf("open source rows: nil reader")
	}
	defer rowReader.Close()

	spec := mapper.MappingSpec{
		FileID:   request.FileID,
		SchemaID: request.SchemaID,
		Sheet:    request.Sheet,
		Mappings: request.Mappings,
	}
	var plan *ExecutionPlan
	if len(columns) == 0 {
		plan, err = CompilePlan(spec, schema)
	} else {
		plan, err = CompilePlan(spec, schema, len(columns))
	}
	if err != nil {
		return result, err
	}

	info := mapper.ImportContext{FileID: request.FileID, SchemaID: request.SchemaID}
	if beginner, ok := processor.(mapper.ImportBeginner); ok {
		if err := beginner.BeginImport(ctx, info); err != nil {
			return result, fmt.Errorf("begin import: %w", err)
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		row, err := rowReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("read source row: %w", err)
		}
		result.Processed++
		rowNumber := int64(row.Number)
		if rowNumber <= 0 {
			rowNumber = result.Processed
		}

		record, err := plan.Execute(sourceValuesWithColumns(row, columns))
		if err != nil {
			result.Failed++
			appendError(&result, rowErrorFrom(err, rowNumber))
			if request.FailFast {
				break
			}
			continue
		}

		if err := processor.Process(ctx, mapper.RowContext{
			FileID:   request.FileID,
			SchemaID: request.SchemaID,
			Row:      rowNumber,
		}, record); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			result.Failed++
			appendError(&result, rowError(CodeProcessorError, err.Error(), rowNumber, nil, nil))
			if request.FailFast {
				break
			}
			continue
		}
		result.Succeeded++
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if ender, ok := processor.(mapper.ImportEnder); ok {
		if err := ender.EndImport(ctx, result); err != nil {
			return result, fmt.Errorf("end import: %w", err)
		}
	}
	return result, nil
}

// Execute runs an import through an Executor.
func (e *Executor) Execute(ctx context.Context, request mapper.ImportRequest) (mapper.ImportResult, error) {
	return e.Import(ctx, request)
}

// ExecuteImport is a package-level convenience for callers that already have
// an executor configured.
func ExecuteImport(ctx context.Context, executor *Executor, request mapper.ImportRequest) (mapper.ImportResult, error) {
	if executor == nil {
		return mapper.ImportResult{}, ErrExecutorUnavailable
	}
	return executor.Import(ctx, request)
}
func appendError(result *mapper.ImportResult, err mapper.RowError) {
	if len(result.Errors) < MaxErrors {
		result.Errors = append(result.Errors, err)
	}
}

func (e *Executor) sourceColumns() []string {
	if len(e.SourceColumns) != 0 {
		return e.SourceColumns
	}
	if provider, ok := e.Source.(interface{ Columns() []string }); ok {
		return provider.Columns()
	}
	return nil
}

func (e *Executor) columnsFromSource(ctx context.Context, reader io.Reader, sheet int) ([]string, error) {
	if columns := e.sourceColumns(); len(columns) != 0 {
		return columns, nil
	}
	seeker, ok := reader.(io.ReadSeeker)
	if !ok {
		return nil, nil
	}
	analysis, err := e.Source.Analyze(ctx, seeker)
	if err != nil {
		return nil, fmt.Errorf("analyze source for columns: %w", err)
	}
	if sheet >= len(analysis.Sheets) {
		return nil, mappingError(CodeInvalidMapping, fmt.Sprintf("sheet index %d does not exist", sheet), nil, nil)
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind source: %w", err)
	}
	return append([]string(nil), analysis.Sheets[sheet].Columns...), nil
}

type sheetOpener interface {
	OpenSheet(context.Context, io.Reader, int) (mapper.RowReader, error)
}

func openRows(ctx context.Context, source mapper.SourceAdapter, reader io.Reader, sheet int) (mapper.RowReader, error) {
	if opener, ok := source.(sheetOpener); ok {
		return opener.OpenSheet(ctx, reader, sheet)
	}
	return source.Open(ctx, reader)
}

func sourceValuesWithColumns(row mapper.SourceRow, columns []string) []string {
	if len(columns) != 0 {
		values := make([]string, len(columns))
		matched := 0
		for index, column := range columns {
			value, ok := row.Values[column]
			if ok {
				matched++
			}
			values[index] = value
		}
		if matched != 0 || len(row.Values) == 0 {
			return values
		}
	}
	return sourceValues(row)
}

func sourceValues(row mapper.SourceRow) []string {
	if len(row.Values) == 0 {
		return nil
	}

	indexed := make(map[int]string, len(row.Values))
	maxIndex := -1
	allIndexed := true
	for key, value := range row.Values {
		index, ok := parseSourceKey(key)
		if !ok {
			allIndexed = false
			break
		}
		indexed[index] = value
		if index > maxIndex {
			maxIndex = index
		}
	}
	if allIndexed && maxIndex >= 0 {
		values := make([]string, maxIndex+1)
		for index, value := range indexed {
			values[index] = value
		}
		return values
	}

	keys := make([]string, 0, len(row.Values))
	for key := range row.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, len(keys))
	for index, key := range keys {
		values[index] = row.Values[key]
	}
	return values
}

func parseSourceKey(key string) (int, bool) {
	for _, prefix := range []string{"col_", "column_"} {
		if strings.HasPrefix(key, prefix) {
			key = strings.TrimPrefix(key, prefix)
			break
		}
	}
	index, err := strconv.Atoi(key)
	return index, err == nil && index >= 0
}
