package mapper

import (
	"context"
	"io"
	"strings"
	"testing"
)

type testFileStore struct{}

func (testFileStore) Open(context.Context, FileID) (io.ReadCloser, FileMetadata, error) {
	return io.NopCloser(strings.NewReader("data")), FileMetadata{ID: "file-1", Name: "data.csv"}, nil
}

type testSourceAdapter struct{}

func (testSourceAdapter) Analyze(context.Context, io.Reader) (SourceAnalysis, error) {
	return SourceAnalysis{Sheets: []SheetAnalysis{{Name: "data", Columns: []string{"value"}}}}, nil
}

func (testSourceAdapter) Open(context.Context, io.Reader) (RowReader, error) { return nil, nil }

func TestServiceAnalyzeFile(t *testing.T) {
	service := New(WithFileStore(testFileStore{}), WithSourceAdapter(testSourceAdapter{}))
	analysis, err := service.AnalyzeFile(context.Background(), "file-1")
	if err != nil {
		t.Fatalf("AnalyzeFile() error = %v", err)
	}
	if analysis.File.ID != "file-1" {
		t.Fatalf("analysis file id = %q, want file-1", analysis.File.ID)
	}
	if len(analysis.Sheets) != 1 || analysis.Sheets[0].Name != "data" {
		t.Fatalf("analysis = %#v", analysis)
	}
}
