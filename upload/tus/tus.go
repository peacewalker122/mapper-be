package tus

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/peacewalker122/mapper/mapper"
)

const (
	Version       = "1.0.0"
	DefaultPrefix = "/uploads/tus"

	FileIDHeader = "X-Mapper-File-Id"
)

type Option func(*Handler)

func WithFileWriter(writer mapper.FileWriter) Option {
	return func(h *Handler) { h.writer = writer }
}

func WithMaxSize(n int64) Option {
	return func(h *Handler) { h.maxSize = n }
}

func WithStagingDir(dir string) Option {
	return func(h *Handler) { h.dir = dir }
}

type session struct {
	length      int64
	offset      int64
	file        *os.File
	path        string
	name        string
	contentType string
}

type Handler struct {
	writer  mapper.FileWriter
	maxSize int64
	dir     string
	owned   bool

	mu        sync.Mutex
	uploads   map[string]*session
	completed map[string]mapper.FileID
}

func New(options ...Option) *Handler {
	h := &Handler{uploads: map[string]*session{}, completed: map[string]mapper.FileID{}}
	for _, opt := range options {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

func (h *Handler) Prefix() string { return DefaultPrefix }

func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.route)
	return mux
}

func (h *Handler) FileID(uploadID string) (mapper.FileID, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.completed[uploadID]
	return id, ok
}

func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, s := range h.uploads {
		if s.file != nil {
			s.file.Close()
		}
		os.Remove(s.path)
		delete(h.uploads, id)
	}
	if !h.owned || h.dir == "" {
		return nil
	}
	dir := h.dir
	h.dir = ""
	return os.RemoveAll(dir)
}

func (h *Handler) ensureDir() (string, error) {
	if h.dir == "" {
		dir, err := os.MkdirTemp("", "mapper-tus-")
		if err != nil {
			return "", err
		}
		h.dir = dir
		h.owned = true
		return dir, nil
	}
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return "", err
	}
	return h.dir, nil
}

func (h *Handler) route(w http.ResponseWriter, r *http.Request) {
	setTusHeaders(w, h.maxSize)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPost:
		if strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/") != "" {
			writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
			return
		}
		h.create(w, r)
	case http.MethodHead:
		h.head(w, r)
	case http.MethodPatch:
		h.patch(w, r)
	case http.MethodDelete:
		h.terminate(w, r)
	default:
		w.Header().Set("Allow", "OPTIONS, POST, HEAD, PATCH, DELETE")
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if h.writer == nil {
		writeErr(w, http.StatusServiceUnavailable, "service_unavailable", "file writer is unavailable")
		return
	}
	if v := r.Header.Get("Tus-Resumable"); v != "" && v != Version {
		writeErr(w, http.StatusBadRequest, "unsupported_version", "Tus-Resumable must be 1.0.0")
		return
	}
	if r.Header.Get("Upload-Defer-Length") != "" {
		writeErr(w, http.StatusBadRequest, "deferred_length_unsupported", "Upload-Defer-Length is not supported; send Upload-Length")
		return
	}
	lengthText := r.Header.Get("Upload-Length")
	if lengthText == "" {
		writeErr(w, http.StatusBadRequest, "length_required", "Upload-Length is required")
		return
	}
	length, err := strconv.ParseInt(lengthText, 10, 64)
	if err != nil || length < 0 {
		writeErr(w, http.StatusBadRequest, "invalid_length", "Upload-Length must be a non-negative integer")
		return
	}
	if h.maxSize > 0 && length > h.maxSize {
		writeErr(w, http.StatusRequestEntityTooLarge, "upload_too_large", "upload length exceeds maximum")
		return
	}
	name, contentType := parseMetadata(r.Header.Get("Upload-Metadata"))

	h.mu.Lock()
	dir, err := h.ensureDir()
	if err != nil {
		h.mu.Unlock()
		writeErr(w, http.StatusInternalServerError, "internal_error", "cannot stage upload")
		return
	}
	id, err := newUploadID()
	if err != nil {
		h.mu.Unlock()
		writeErr(w, http.StatusInternalServerError, "internal_error", "cannot allocate upload")
		return
	}
	path := filepath.Join(dir, id)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		h.mu.Unlock()
		writeErr(w, http.StatusInternalServerError, "internal_error", "cannot stage upload")
		return
	}
	s := &session{length: length, path: path, file: file, name: name, contentType: contentType}
	h.uploads[id] = s
	h.mu.Unlock()

	var firstChunk int64
	if r.ContentLength != 0 {
		if r.Header.Get("Content-Type") != "application/offset+octet-stream" {
			h.abort(id)
			writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "creation content must be application/offset+octet-stream")
			return
		}
		n, err := h.append(id, r.Body, length)
		if err != nil {
			h.abort(id)
			writeErr(w, http.StatusBadRequest, "invalid_chunk", err.Error())
			return
		}
		firstChunk = n
	}

	w.Header().Set("Location", DefaultPrefix+"/"+id)
	w.Header().Set("Upload-Offset", strconv.FormatInt(firstChunk, 10))
	if length == 0 || firstChunk >= length {
		fileID, err := h.finish(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		w.Header().Set(FileIDHeader, string(fileID))
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) head(w http.ResponseWriter, r *http.Request) {
	id, ok := uploadID(r.URL.Path)
	if !ok {
		writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if fileID, done := h.completed[id]; done {
		w.Header().Set(FileIDHeader, string(fileID))
	}
	s, ok := h.uploads[id]
	if !ok {
		if _, done := h.completed[id]; done {
			w.Header().Set("Upload-Offset", "0")
			w.Header().Set("Cache-Control", "no-store")
			return
		}
		writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(s.offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(s.length, 10))
	w.Header().Set("Cache-Control", "no-store")
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	id, ok := uploadID(r.URL.Path)
	if !ok {
		writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
		return
	}
	if v := r.Header.Get("Tus-Resumable"); v != "" && v != Version {
		writeErr(w, http.StatusBadRequest, "unsupported_version", "Tus-Resumable must be 1.0.0")
		return
	}
	if r.Header.Get("Content-Type") != "application/offset+octet-stream" {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "PATCH content must be application/offset+octet-stream")
		return
	}
	want, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || want < 0 {
		writeErr(w, http.StatusBadRequest, "invalid_offset", "Upload-Offset must be a non-negative integer")
		return
	}
	h.mu.Lock()
	s, ok := h.uploads[id]
	if !ok {
		h.mu.Unlock()
		if _, done := h.completed[id]; done {
			writeErr(w, http.StatusBadRequest, "upload_complete", "upload is already complete")
			return
		}
		writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
		return
	}
	if want != s.offset {
		h.mu.Unlock()
		writeErr(w, http.StatusConflict, "offset_mismatch", fmt.Sprintf("Upload-Offset must be %d", s.offset))
		return
	}
	remaining := s.length - s.offset
	h.mu.Unlock()

	n, werr := h.append(id, http.MaxBytesReader(w, r.Body, remaining+1), remaining)
	if werr != nil {
		if werr == errTooLarge {
			writeErr(w, http.StatusRequestEntityTooLarge, "upload_too_large", "chunk exceeds declared Upload-Length")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid_chunk", werr.Error())
		return
	}
	_ = n

	h.mu.Lock()
	s, ok = h.uploads[id]
	if !ok {
		h.mu.Unlock()
		writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
		return
	}
	offset := s.offset
	complete := offset >= s.length
	h.mu.Unlock()

	w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
	if complete {
		fileID, err := h.finish(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		w.Header().Set(FileIDHeader, string(fileID))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) terminate(w http.ResponseWriter, r *http.Request) {
	id, ok := uploadID(r.URL.Path)
	if !ok {
		writeErr(w, http.StatusNotFound, "upload_not_found", "unknown upload")
		return
	}
	h.mu.Lock()
	if s, ok := h.uploads[id]; ok {
		if s.file != nil {
			s.file.Close()
		}
		os.Remove(s.path)
		delete(h.uploads, id)
	}
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

var errTooLarge = fmt.Errorf("chunk exceeds declared upload length")

func (h *Handler) append(id string, src io.Reader, remaining int64) (int64, error) {
	h.mu.Lock()
	s, ok := h.uploads[id]
	h.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("unknown upload")
	}
	limited := io.LimitReader(src, remaining+1)
	n, err := io.Copy(s.file, limited)
	if err != nil {
		return 0, err
	}
	if n > remaining {
		return 0, errTooLarge
	}
	if err := s.file.Sync(); err != nil {
		return 0, err
	}
	h.mu.Lock()
	s.offset += n
	offset := s.offset
	h.mu.Unlock()
	_ = offset
	return n, nil
}

func (h *Handler) finish(id string) (mapper.FileID, error) {
	h.mu.Lock()
	s, ok := h.uploads[id]
	if !ok {
		if fileID, done := h.completed[id]; done {
			h.mu.Unlock()
			return fileID, nil
		}
		h.mu.Unlock()
		return "", fmt.Errorf("unknown upload")
	}
	if s.offset != s.length {
		h.mu.Unlock()
		return "", fmt.Errorf("upload is incomplete")
	}
	path := s.path
	name := s.name
	contentType := s.contentType
	if s.file != nil {
		s.file.Close()
	}
	delete(h.uploads, id)
	h.mu.Unlock()

	if h.writer == nil {
		os.Remove(path)
		return "", fmt.Errorf("file writer is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	defer os.Remove(path)
	saved, err := h.writer.Save(nil, mapper.FileMetadata{
		Name:        name,
		Filename:    name,
		ContentType: contentType,
		Extension:   filepath.Ext(name),
	}, file)
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	h.completed[id] = saved.ID
	h.mu.Unlock()
	return saved.ID, nil
}

func (h *Handler) abort(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.uploads[id]; ok {
		if s.file != nil {
			s.file.Close()
		}
		os.Remove(s.path)
		delete(h.uploads, id)
	}
}

func uploadID(path string) (string, bool) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return "", false
	}
	for _, r := range trimmed {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return "", false
	}
	return trimmed, true
}

func newUploadID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func parseMetadata(header string) (name, contentType string) {
	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, _ := strings.Cut(item, " ")
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		switch strings.ToLower(key) {
		case "filename":
			name = string(decoded)
		case "filetype":
			contentType = string(decoded)
		}
	}
	return name, contentType
}

func setTusHeaders(w http.ResponseWriter, maxSize int64) {
	w.Header().Set("Tus-Resumable", Version)
	w.Header().Set("Tus-Version", Version)
	w.Header().Set("Tus-Extension", "creation,termination")
	if maxSize > 0 {
		w.Header().Set("Tus-Max-Size", strconv.FormatInt(maxSize, 10))
	}
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}
