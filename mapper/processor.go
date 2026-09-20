package mapper

import "context"

// ImportProcessor receives one converted record at a time.
type ImportProcessor interface {
	Process(context.Context, RowContext, Record) error
}

// ImportProcessorFunc adapts a function to ImportProcessor.
type ImportProcessorFunc func(context.Context, RowContext, Record) error

func (f ImportProcessorFunc) Process(ctx context.Context, info RowContext, record Record) error {
	if f == nil {
		return nil
	}
	return f(ctx, info, record)
}

// Func is a short alias for ImportProcessorFunc.
type Func = ImportProcessorFunc

// ImportBeginner is an optional import lifecycle hook.
type ImportBeginner interface {
	BeginImport(context.Context, ImportContext) error
}

// ImportEnder is an optional import lifecycle hook.
type ImportEnder interface {
	EndImport(context.Context, ImportResult) error
}

// Beginner and Ender are short aliases for the optional lifecycle hooks.
type Beginner = ImportBeginner
type Ender = ImportEnder

// ImportContext identifies one import for lifecycle hooks.
type ImportContext struct {
	FileID   FileID `json:"file_id"`
	SchemaID uint64 `json:"schema_id"`
}
