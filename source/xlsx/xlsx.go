package xlsx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/peacewalker122/mapper/source"
	"github.com/xuri/excelize/v2"
)

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

var _ source.SourceAdapter = (*Adapter)(nil)

func (Adapter) Analyze(ctx context.Context, input io.Reader) (source.SourceAnalysis, error) {
	if input == nil {
		return source.SourceAnalysis{}, errors.New("xlsx: nil reader")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return source.SourceAnalysis{}, err
	}

	file, err := excelize.OpenReader(input)
	if err != nil {
		return source.SourceAnalysis{}, fmt.Errorf("xlsx: open workbook: %w", err)
	}
	defer file.Close()

	analysis := source.SourceAnalysis{}
	for _, name := range file.GetSheetList() {
		sheet, ok, err := analyzeSheet(ctx, file, name)
		if err != nil {
			return source.SourceAnalysis{}, err
		}
		if ok {
			analysis.Sheets = append(analysis.Sheets, sheet)
		}
	}
	if len(analysis.Sheets) == 0 {
		return source.SourceAnalysis{}, source.ErrNoHeader
	}
	return analysis, nil
}

func (Adapter) Open(ctx context.Context, input io.Reader) (source.RowReader, error) {
	if input == nil {
		return nil, errors.New("xlsx: nil reader")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("xlsx: open workbook: %w", err)
	}
	reader, err := openSheet(ctx, file)
	if err != nil {
		file.Close()
		return nil, err
	}
	return reader, nil
}

type rowReader struct {
	file     *excelize.File
	rows     *excelize.Rows
	columns  []string
	ctx      context.Context
	rowIndex int
	closed   bool
}

func (r *rowReader) Next() (source.SourceRow, error) {
	if r.closed {
		return source.SourceRow{}, io.EOF
	}
	if err := r.ctx.Err(); err != nil {
		return source.SourceRow{}, err
	}
	if !r.rows.Next() {
		if err := r.rows.Error(); err != nil {
			_ = r.Close()
			return source.SourceRow{}, fmt.Errorf("xlsx: read rows: %w", err)
		}
		if err := r.Close(); err != nil {
			return source.SourceRow{}, err
		}
		return source.SourceRow{}, io.EOF
	}

	values, err := r.rows.Columns()
	if err != nil {
		_ = r.Close()
		return source.SourceRow{}, fmt.Errorf("xlsx: read row: %w", err)
	}
	r.rowIndex++
	return source.SourceRow{Number: r.rowIndex, Values: rowValues(r.columns, values)}, nil
}

func (r *rowReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	rowsErr := r.rows.Close()
	fileErr := r.file.Close()
	if rowsErr != nil {
		return rowsErr
	}
	return fileErr
}

func openSheet(ctx context.Context, file *excelize.File) (*rowReader, error) {
	for _, name := range file.GetSheetList() {
		rows, err := file.Rows(name)
		if err != nil {
			return nil, fmt.Errorf("xlsx: open sheet %q: %w", name, err)
		}
		rowIndex := 0
		for rows.Next() {
			rowIndex++
			if err := ctx.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			values, err := rows.Columns()
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("xlsx: read header from sheet %q: %w", name, err)
			}
			if !hasValue(values) {
				continue
			}
			return &rowReader{
				file:     file,
				rows:     rows,
				columns:  append([]string(nil), values...),
				ctx:      ctx,
				rowIndex: rowIndex,
			}, nil
		}
		if err := rows.Error(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("xlsx: read sheet %q: %w", name, err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("xlsx: close sheet %q: %w", name, err)
		}
	}
	return nil, source.ErrNoHeader
}

func analyzeSheet(ctx context.Context, file *excelize.File, name string) (source.SheetAnalysis, bool, error) {
	rows, err := file.Rows(name)
	if err != nil {
		return source.SheetAnalysis{}, false, fmt.Errorf("xlsx: open sheet %q: %w", name, err)
	}
	defer rows.Close()

	var sheet source.SheetAnalysis
	headerFound := false
	rowIndex := 0
	for rows.Next() {
		rowIndex++
		if err := ctx.Err(); err != nil {
			return source.SheetAnalysis{}, false, err
		}
		values, err := rows.Columns()
		if err != nil {
			return source.SheetAnalysis{}, false, fmt.Errorf("xlsx: read sheet %q: %w", name, err)
		}
		if !headerFound {
			if !hasValue(values) {
				continue
			}
			sheet.Name = name
			sheet.Columns = append([]string(nil), values...)
			headerFound = true
			continue
		}
		sheet.Rows++
		if len(sheet.Samples) < source.DefaultSampleLimit {
			sheet.Samples = append(sheet.Samples, source.SourceRow{
				Number: rowIndex,
				Values: rowValues(sheet.Columns, values),
			})
		}
	}
	if err := rows.Error(); err != nil {
		return source.SheetAnalysis{}, false, fmt.Errorf("xlsx: read sheet %q: %w", name, err)
	}
	return sheet, headerFound, nil
}

func hasValue(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func rowValues(columns, values []string) map[string]string {
	result := make(map[string]string, len(columns))
	for index, column := range columns {
		if index < len(values) {
			result[column] = values[index]
			continue
		}
		result[column] = ""
	}
	return result
}
