package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/peacewalker122/mapper/executor"
	"github.com/peacewalker122/mapper/filestore/tempfile"
	"github.com/peacewalker122/mapper/mapper"
	"github.com/peacewalker122/mapper/mapperhttp"
	"github.com/peacewalker122/mapper/source/csv"
)

const e2eCSV = `phone,age,score,active,joined,status
628123456789,30,9.5,true,2024-01-02T15:04:05Z,ACTIVE
628000000001,notanint,1.5,false,2024-01-03T00:00:00Z,ACTIVE
,25,2.5,true,2024-01-04T00:00:00Z,INACTIVE
628999999999,,,,,ACTIVE
`

type collector struct {
	mu      sync.Mutex
	records []mapper.Record
	infos   []mapper.RowContext
}

func (c *collector) Process(_ context.Context, info mapper.RowContext, record mapper.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record)
	c.infos = append(c.infos, info)
	return nil
}

func (c *collector) all() ([]mapper.Record, []mapper.RowContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mapper.Record(nil), c.records...), append([]mapper.RowContext(nil), c.infos...)
}

func compileE2ESchema(t *testing.T) mapper.Schema {
	t.Helper()
	return mapper.Schema{
		ID:   1001,
		Name: "subscriber",
		Fields: []mapper.Field{
			{ID: 1002, Name: "phone", Type: mapper.TypeString, Required: true},
			{ID: 1003, Name: "age", Type: mapper.TypeInteger},
			{ID: 1004, Name: "score", Type: mapper.TypeDecimal},
			{ID: 1005, Name: "active", Type: mapper.TypeBoolean},
			{ID: 1006, Name: "joined", Type: mapper.TypeDateTime},
			{ID: 1007, Name: "status", Type: mapper.TypeString, Required: true},
		},
	}
}

func postMultipartCSV(t *testing.T, url, filename, data string) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := io.WriteString(part, data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, respBody
}

func postJSON(t *testing.T, url string, payload any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, respBody
}

func TestFullSystem(t *testing.T) {
	schema := compileE2ESchema(t)

	registry := mapper.NewMemorySchemaRegistry()
	store := tempfile.New(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	collected := &collector{}
	imp := executor.NewImportExecutor(registry, store, csv.New(), collected)
	svc := mapper.New(
		mapper.WithSchemaRegistry(registry),
		mapper.WithFileStore(store),
		mapper.WithSourceAdapter(csv.New()),
		mapper.WithImporter(imp),
	)
	if err := svc.RegisterSchema(schema); err != nil {
		t.Fatalf("register schema: %v", err)
	}

	handler := mapperhttp.New(svc, store)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// 1. Upload + analyze via multipart convenience endpoint.
	status, body := postMultipartCSV(t, srv.URL+"/files/analyze", "contacts.csv", e2eCSV)
	if status != http.StatusOK {
		t.Fatalf("analyze status = %d, body = %s", status, body)
	}
	var analysis mapper.SourceAnalysis
	if err := json.Unmarshal(body, &analysis); err != nil {
		t.Fatalf("decode analysis: %v\nbody: %s", err, body)
	}
	if !analysis.File.ID.Valid() {
		t.Fatalf("analysis file id invalid: %+v", analysis.File)
	}
	if len(analysis.Sheets) == 0 {
		t.Fatalf("no sheets in analysis: %s", body)
	}
	gotCols := analysis.Sheets[0].Columns
	wantCols := []string{"phone", "age", "score", "active", "joined", "status"}
	if len(gotCols) != len(wantCols) {
		t.Fatalf("columns = %v, want %v", gotCols, wantCols)
	}
	for i := range wantCols {
		if gotCols[i] != wantCols[i] {
			t.Fatalf("columns = %v, want %v", gotCols, wantCols)
		}
	}
	fileID := analysis.File.ID

	// 2. Retrieve target schema.
	resp, err := http.Get(srv.URL + "/schemas/" + uintToString(schema.ID))
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}
	schemaBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read schema body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("schema status = %d, body = %s", resp.StatusCode, schemaBody)
	}
	var gotSchema mapper.Schema
	if err := json.Unmarshal(schemaBody, &gotSchema); err != nil {
		t.Fatalf("decode schema: %v\nbody: %s", err, schemaBody)
	}
	if gotSchema.ID != schema.ID || gotSchema.Name != schema.Name || len(gotSchema.Fields) != len(schema.Fields) {
		t.Fatalf("schema mismatch: got %+v, want %+v", gotSchema, schema)
	}
	for i := range schema.Fields {
		if gotSchema.Fields[i].ID != schema.Fields[i].ID || gotSchema.Fields[i].Name != schema.Fields[i].Name {
			t.Fatalf("schema field %d mismatch: got %+v, want %+v", i, gotSchema.Fields[i], schema.Fields[i])
		}
	}

	// 3. Submit mapping: every column feeds its same-named target.
	targetByName := map[string]uint64{}
	for _, f := range schema.Fields {
		targetByName[f.Name] = f.ID
	}
	var mappings []mapper.FieldMapping
	for idx, col := range gotCols {
		id, ok := targetByName[col]
		if !ok {
			t.Fatalf("column %q has no target field", col)
		}
		mappings = append(mappings, mapper.FieldMapping{Source: idx, Target: id})
	}
	importPayload := map[string]any{
		"file_id":   fileID,
		"schema_id": schema.ID,
		"sheet":     0,
		"mappings":  mappings,
	}
	status, body = postJSON(t, srv.URL+"/imports/sync", importPayload)
	if status != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", status, body)
	}
	var result mapper.ImportResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode result: %v\nbody: %s", err, body)
	}
	if result.Processed != 4 {
		t.Fatalf("processed = %d, want 4", result.Processed)
	}
	if result.Succeeded != 2 {
		t.Fatalf("succeeded = %d, want 2: %+v", result.Succeeded, result)
	}
	if result.Failed != 2 {
		t.Fatalf("failed = %d, want 2: %+v", result.Failed, result)
	}
	codes := map[string]bool{}
	for _, e := range result.Errors {
		codes[e.Code] = true
	}
	if !codes["invalid_integer"] {
		t.Fatalf("missing invalid_integer error: %+v", result.Errors)
	}
	if !codes["required_value_missing"] {
		t.Fatalf("missing required_value_missing error: %+v", result.Errors)
	}
	if len(result.Errors) > 1000 {
		t.Fatalf("errors exceed bound: %d", len(result.Errors))
	}

	// 4. Processor received typed records in schema field order.
	records, infos := collected.all()
	if len(records) != 2 {
		t.Fatalf("processor records = %d, want 2", len(records))
	}
	// Row 1: fully typed values.
	r1 := records[0].Values
	if len(r1) != 6 {
		t.Fatalf("record values = %d, want 6", len(r1))
	}
	if r1[0] != "628123456789" {
		t.Fatalf("phone = %#v", r1[0])
	}
	if r1[1] != int64(30) {
		t.Fatalf("age = %#v, want int64(30)", r1[1])
	}
	if r1[2] != 9.5 {
		t.Fatalf("score = %#v, want 9.5", r1[2])
	}
	if r1[3] != true {
		t.Fatalf("active = %#v, want true", r1[3])
	}
	joined, ok := r1[4].(time.Time)
	if !ok {
		t.Fatalf("joined = %#v, want time.Time", r1[4])
	}
	if joined.Year() != 2024 || joined.Month() != time.January || joined.Day() != 2 {
		t.Fatalf("joined = %v", joined)
	}
	if r1[5] != "ACTIVE" {
		t.Fatalf("status = %#v", r1[5])
	}
	// Row 4: empty optionals decode to nil, requireds intact.
	r4 := records[1].Values
	if r4[0] != "628999999999" || r4[5] != "ACTIVE" {
		t.Fatalf("row4 requireds = %#v", r4)
	}
	for i, name := range []string{"age", "score", "active", "joined"} {
		if r4[i+1] != nil {
			t.Fatalf("row4 %s = %#v, want nil", name, r4[i+1])
		}
	}
	for _, info := range infos {
		if info.FileID != fileID || info.SchemaID != schema.ID {
			t.Fatalf("row context = %+v", info)
		}
		if info.Row <= 0 {
			t.Fatalf("row number = %d, want > 0", info.Row)
		}
	}

	// 5. Negative: required target left unmapped must fail, not import.
	badPayload := map[string]any{
		"file_id":   fileID,
		"schema_id": schema.ID,
		"sheet":     0,
		"mappings":  mappings[1:],
	}
	status, body = postJSON(t, srv.URL+"/imports/sync", badPayload)
	if status == http.StatusOK {
		t.Fatalf("expected failure for unmapped required field, got 200: %s", body)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v\nbody: %s", err, body)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj == nil || errObj["code"] == nil {
		t.Fatalf("missing error.code envelope: %s", body)
	}
}

func uintToString(v uint64) string {
	if v == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for n := v; n > 0; n /= 10 {
		pos--
		digits[pos] = byte('0' + n%10)
	}
	return string(digits[pos:])
}
