// Package mapperhttp exposes mapper service operations over net/http.
package mapperhttp

import (
	"net/http"
	"strings"

	"github.com/peacewalker122/mapper/mapper"
	"github.com/peacewalker122/mapper/upload"
)

const (
	SchemasPath = "/schemas/"
	AnalyzePath = "/files/analyze"
	ImportPath  = "/imports/sync"
)

// Handler is the HTTP adapter for a mapper service.
type Handler struct {
	service    *mapper.Service
	writer     mapper.FileWriter
	extensions []upload.HTTPUploadExtension
	suggester  Suggester
	mux        *http.ServeMux
}

// Option configures Handler.
type Option func(*Handler)

// WithFileWriter enables direct multipart uploads on files/analyze.
func WithFileWriter(writer mapper.FileWriter) Option {
	return func(handler *Handler) { handler.writer = writer }
}

// WithFileStore enables direct multipart uploads when store also implements
// mapper.FileWriter.
func WithFileStore(store mapper.FileStore) Option {
	return func(handler *Handler) {
		if writer, ok := store.(mapper.FileWriter); ok {
			handler.writer = writer
		}
	}
}

// WithUploadExtension mounts an application-owned upload handler below its
// declared prefix.
func WithUploadExtension(extension upload.HTTPUploadExtension) Option {
	return func(handler *Handler) {
		if extension != nil {
			handler.extensions = append(handler.extensions, extension)
		}
	}
}

// WithUpload is a short alias for WithUploadExtension.
func WithUpload(extension upload.HTTPUploadExtension) Option {
	return WithUploadExtension(extension)
}

// New constructs an HTTP handler. Components may be Option values, a
// mapper.FileWriter, a mapper.FileStore that also writes, or an upload
// extension. Variadic components keep the upload and storage integrations
// optional without adding separate constructors.
func New(service *mapper.Service, components ...any) *Handler {
	handler := &Handler{service: service}
	for _, component := range components {
		switch value := component.(type) {
		case Option:
			if value != nil {
				value(handler)
			}
		case Suggester:
			handler.suggester = value
		case mapper.FileWriter:
			handler.writer = value
		case mapper.FileStore:
			if writer, ok := value.(mapper.FileWriter); ok {
				handler.writer = writer
			}
		case upload.HTTPUploadExtension:
			if value != nil {
				handler.extensions = append(handler.extensions, value)
			}
		case []upload.HTTPUploadExtension:
			for _, extension := range value {
				if extension != nil {
					handler.extensions = append(handler.extensions, extension)
				}
			}
		}
	}
	handler.mount()
	return handler
}

// NewWithOptions is the strongly typed constructor for callers using options.
func NewWithOptions(service *mapper.Service, options ...Option) *Handler {
	components := make([]any, len(options))
	for index, option := range options {
		components[index] = option
	}
	return New(service, components...)
}

// ServeHTTP implements http.Handler.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if handler == nil || handler.mux == nil {
		writeErrorStatus(writer, http.StatusServiceUnavailable, "handler_unavailable", "mapper HTTP handler is unavailable")
		return
	}
	handler.mux.ServeHTTP(writer, request)
}

// Handler returns handler itself for APIs that expect an explicit
// http.Handler accessor.
func (handler *Handler) Handler() http.Handler { return handler }

func (handler *Handler) mount() {
	handler.mux = http.NewServeMux()
	handler.mux.Handle(SchemasPath, http.StripPrefix("/schemas", http.HandlerFunc(handler.handleSchema)))
	handler.mux.Handle(AnalyzePath, http.HandlerFunc(handler.handleAnalyze))
	handler.mux.Handle(ImportPath, http.HandlerFunc(handler.handleImport))
	handler.mux.Handle(SuggestPath, http.HandlerFunc(handler.handleSuggest))
	for _, extension := range handler.extensions {
		mountExtension(handler.mux, extension)
	}
	handler.mux.Handle("/", http.HandlerFunc(handler.notFound))
}

func mountExtension(mux *http.ServeMux, extension upload.HTTPUploadExtension) {
	if extension == nil || mux == nil {
		return
	}
	prefix := strings.TrimSpace(extension.Prefix())
	if prefix == "" || prefix == "/" {
		return
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return
	}
	handler := extension.Handler()
	if handler == nil {
		return
	}
	mounted := http.StripPrefix(prefix, handler)
	mux.Handle(prefix, mounted)
	mux.Handle(prefix+"/", mounted)
}

func (handler *Handler) notFound(writer http.ResponseWriter, request *http.Request) {
	writeErrorStatus(writer, http.StatusNotFound, "not_found", "route not found")
}
