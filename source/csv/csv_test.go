package csv

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/peacewalker122/mapper/source"
)

func TestAdapterAnalyzesAndStreamsRows(t *testing.T) {
	input := "\n,\nname,email\nAda,ada@example.com\nGrace,grace@example.com\n"
	adapter := New()

	analysis, err := adapter.Analyze(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	wantAnalysis := source.SourceAnalysis{
		Sheets: []source.SheetAnalysis{{
			Name:    "csv",
			Columns: []string{"name", "email"},
			Samples: []source.SourceRow{
				{Number: 3, Values: map[string]string{"name": "Ada", "email": "ada@example.com"}},
				{Number: 4, Values: map[string]string{"name": "Grace", "email": "grace@example.com"}},
			},
			Rows: 2,
		}},
	}
	if !reflect.DeepEqual(analysis, wantAnalysis) {
		t.Fatalf("analysis = %#v, want %#v", analysis, wantAnalysis)
	}

	rows, err := adapter.Open(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rows.Close()
	for _, want := range wantAnalysis.Sheets[0].Samples {
		got, err := rows.Next()
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("row = %#v, want %#v", got, want)
		}
	}
	if _, err := rows.Next(); err != io.EOF {
		t.Fatalf("final Next() error = %v, want EOF", err)
	}
}

func TestAdapterRejectsEmptyInput(t *testing.T) {
	_, err := New().Analyze(context.Background(), strings.NewReader("\n,\n"))
	if err != source.ErrNoHeader {
		t.Fatalf("Analyze() error = %v, want %v", err, source.ErrNoHeader)
	}
}
