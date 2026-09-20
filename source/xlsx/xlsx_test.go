package xlsx

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/peacewalker122/mapper/source"
	"github.com/xuri/excelize/v2"
)

func workbook(t *testing.T) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer file.Close()
	if err := file.SetSheetRow("Sheet1", "A1", &[]any{"name", "email"}); err != nil {
		t.Fatal(err)
	}
	if err := file.SetSheetRow("Sheet1", "A2", &[]any{"Ada", "ada@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := file.SetSheetRow("Sheet1", "A3", &[]any{"Grace", "grace@example.com"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := file.Write(&output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestAdapterAnalyzesAndStreamsRows(t *testing.T) {
	data := workbook(t)
	adapter := New()

	analysis, err := adapter.Analyze(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	want := source.SourceAnalysis{Sheets: []source.SheetAnalysis{{
		Name:    "Sheet1",
		Columns: []string{"name", "email"},
		Samples: []source.SourceRow{
			{Number: 2, Values: map[string]string{"name": "Ada", "email": "ada@example.com"}},
			{Number: 3, Values: map[string]string{"name": "Grace", "email": "grace@example.com"}},
		},
		Rows: 2,
	}}}
	if !reflect.DeepEqual(analysis, want) {
		t.Fatalf("analysis = %#v, want %#v", analysis, want)
	}

	rows, err := adapter.Open(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rows.Close()
	for _, wantRow := range want.Sheets[0].Samples {
		got, err := rows.Next()
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !reflect.DeepEqual(got, wantRow) {
			t.Fatalf("row = %#v, want %#v", got, wantRow)
		}
	}
	if _, err := rows.Next(); err != io.EOF {
		t.Fatalf("final Next() error = %v, want EOF", err)
	}
}
