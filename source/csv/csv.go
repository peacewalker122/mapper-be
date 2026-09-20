package csv

import (
	"context"
	encodingcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/peacewalker122/mapper/source"
)

const sheetName = "csv"

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

var _ source.SourceAdapter = (*Adapter)(nil)

func (Adapter) Analyze(ctx context.Context, input io.Reader) (source.SourceAnalysis, error) {
	reader, err := (Adapter{}).Open(ctx, input)
	if err != nil {
		return source.SourceAnalysis{}, err
	}
	defer reader.Close()

	analysis := source.SourceAnalysis{
		Sheets: []source.SheetAnalysis{{Name: sheetName}},
	}
	analysis.Sheets[0].Columns = append([]string(nil), reader.(*rowReader).columns...)

	for {
		row, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return source.SourceAnalysis{}, err
		}
		analysis.Sheets[0].Rows++
		if len(analysis.Sheets[0].Samples) < source.DefaultSampleLimit {
			analysis.Sheets[0].Samples = append(analysis.Sheets[0].Samples, row)
		}
	}
	return analysis, nil
}

func (Adapter) Open(ctx context.Context, input io.Reader) (source.RowReader, error) {
	if input == nil {
		return nil, errors.New("csv: nil reader")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reader := &rowReader{reader: encodingcsv.NewReader(input), ctx: ctx}
	reader.reader.FieldsPerRecord = -1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := reader.reader.Read()
		if errors.Is(err, io.EOF) {
			return nil, source.ErrNoHeader
		}
		if err != nil {
			return nil, fmt.Errorf("csv: read header: %w", err)
		}
		reader.rowNumber++
		if !hasValue(record) {
			continue
		}
		reader.columns = append([]string(nil), record...)
		if len(reader.columns) > 0 {
			reader.columns[0] = strings.TrimPrefix(reader.columns[0], "\ufeff")
		}
		return reader, nil
	}
}

type rowReader struct {
	reader    *encodingcsv.Reader
	columns   []string
	ctx       context.Context
	rowNumber int
	done      bool
}

func (r *rowReader) Next() (source.SourceRow, error) {
	if r.done {
		return source.SourceRow{}, io.EOF
	}
	if err := r.ctx.Err(); err != nil {
		return source.SourceRow{}, err
	}

	record, err := r.reader.Read()
	if errors.Is(err, io.EOF) {
		r.done = true
		return source.SourceRow{}, io.EOF
	}
	if err != nil {
		return source.SourceRow{}, fmt.Errorf("csv: read row: %w", err)
	}
	r.rowNumber++
	return source.SourceRow{Number: r.rowNumber, Values: values(r.columns, record)}, nil
}

func (r *rowReader) Close() error {
	r.done = true
	return nil
}

func hasValue(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func values(columns, record []string) map[string]string {
	result := make(map[string]string, len(columns))
	for index, column := range columns {
		if index < len(record) {
			result[column] = record[index]
			continue
		}
		result[column] = ""
	}
	return result
}
