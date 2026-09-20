package tus

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peacewalker122/mapper/filestore/tempfile"
	"github.com/peacewalker122/mapper/mapper"
)

func newTestHandler(t *testing.T, opts ...Option) (*Handler, *tempfile.Store) {
	t.Helper()
	store := tempfile.New(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	base := []Option{WithFileWriter(store)}
	h := New(append(base, opts...)...)
	t.Cleanup(func() { _ = h.Close() })
	return h, store
}

func serve(h *Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, req)
	return rec
}

func metadata(filename, filetype string) string {
	return "filename " + base64.StdEncoding.EncodeToString([]byte(filename)) +
		",filetype " + base64.StdEncoding.EncodeToString([]byte(filetype))
}

func TestOptions(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := serve(h, http.MethodOptions, "/", nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Tus-Resumable") != Version {
		t.Fatalf("missing Tus-Resumable: %v", rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Tus-Extension"), "creation") {
		t.Fatalf("missing creation extension: %v", rec.Header())
	}
}

func TestResumableFlow(t *testing.T) {
	h, store := newTestHandler(t)
	data := "hello world"
	headers := map[string]string{
		"Tus-Resumable":   Version,
		"Upload-Length":   "11",
		"Upload-Metadata": metadata("contacts.csv", "text/csv"),
	}
	rec := serve(h, http.MethodPost, "/", nil, headers)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, DefaultPrefix+"/") {
		t.Fatalf("location = %q", loc)
	}
	id := strings.TrimPrefix(loc, DefaultPrefix+"/")
	if rec.Header().Get("Upload-Offset") != "0" {
		t.Fatalf("offset = %q", rec.Header().Get("Upload-Offset"))
	}

	rec = serve(h, http.MethodHead, "/"+id, nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Upload-Offset") != "0" {
		t.Fatalf("head = %d %q", rec.Code, rec.Header().Get("Upload-Offset"))
	}

	patch := func(offset int, chunk string) *httptest.ResponseRecorder {
		return serve(h, http.MethodPatch, "/"+id, strings.NewReader(chunk), map[string]string{
			"Tus-Resumable": Version,
			"Content-Type":  "application/offset+octet-stream",
			"Upload-Offset": itoa(offset),
		})
	}
	rec = patch(0, "hello")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("patch1 = %d %q %s", rec.Code, rec.Header().Get("Upload-Offset"), rec.Body.String())
	}
	if fid := rec.Header().Get(FileIDHeader); fid != "" {
		t.Fatalf("incomplete upload advertised file id %q", fid)
	}

	rec = serve(h, http.MethodHead, "/"+id, nil, nil)
	if rec.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("resumed offset = %q", rec.Header().Get("Upload-Offset"))
	}

	rec = patch(5, " world")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("patch2 = %d %s", rec.Code, rec.Body.String())
	}
	fileID := rec.Header().Get(FileIDHeader)
	if fileID == "" {
		t.Fatal("completed upload missing file id header")
	}
	if got, ok := h.FileID(id); !ok || string(got) != fileID {
		t.Fatalf("FileID lookup = %q,%v", got, ok)
	}

	reader, meta, err := store.Open(t.Context(), mapper.FileID(fileID))
	if err != nil {
		t.Fatalf("open stored file: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(content) != data {
		t.Fatalf("content = %q, want %q", content, data)
	}
	if meta.Name != "contacts.csv" {
		t.Fatalf("name = %q", meta.Name)
	}
}

func TestOffsetMismatch(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := serve(h, http.MethodPost, "/", nil, map[string]string{"Upload-Length": "5"})
	id := strings.TrimPrefix(rec.Header().Get("Location"), DefaultPrefix+"/")
	rec = serve(h, http.MethodPatch, "/"+id, strings.NewReader("hello"), map[string]string{
		"Content-Type":  "application/offset+octet-stream",
		"Upload-Offset": "3",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestWrongContentType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := serve(h, http.MethodPost, "/", nil, map[string]string{"Upload-Length": "5"})
	id := strings.TrimPrefix(rec.Header().Get("Location"), DefaultPrefix+"/")
	rec = serve(h, http.MethodPatch, "/"+id, strings.NewReader("hello"), map[string]string{
		"Content-Type":  "text/plain",
		"Upload-Offset": "0",
	})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestTerminate(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := serve(h, http.MethodPost, "/", nil, map[string]string{"Upload-Length": "5"})
	id := strings.TrimPrefix(rec.Header().Get("Location"), DefaultPrefix+"/")
	rec = serve(h, http.MethodDelete, "/"+id, nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = serve(h, http.MethodHead, "/"+id, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("head after delete = %d, want 404", rec.Code)
	}
}

func TestMaxSizeAndEmptyFile(t *testing.T) {
	h, _ := newTestHandler(t, WithMaxSize(4))
	rec := serve(h, http.MethodPost, "/", nil, map[string]string{"Upload-Length": "5"})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d, want 413", rec.Code)
	}

	h2, _ := newTestHandler(t)
	rec = serve(h2, http.MethodPost, "/", nil, map[string]string{
		"Upload-Length":   "0",
		"Upload-Metadata": metadata("empty.csv", "text/csv"),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("empty create status = %d", rec.Code)
	}
	if rec.Header().Get(FileIDHeader) == "" {
		t.Fatal("empty upload missing file id")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for v := n; v > 0; v /= 10 {
		pos--
		digits[pos] = byte('0' + v%10)
	}
	return string(digits[pos:])
}
