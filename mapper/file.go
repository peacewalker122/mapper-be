package mapper

import (
	"context"
	"io"
	"time"
)

// FileID identifies an uploaded file in a FileStore.
type FileID string

func (id FileID) String() string { return string(id) }

func (id FileID) Valid() bool { return id != "" }

// FileMetadata describes a stored input file without exposing its storage path.
type FileMetadata struct {
	ID          FileID    `json:"id"`
	Name        string    `json:"name"`
	Filename    string    `json:"filename,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	Extension   string    `json:"extension,omitempty"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// FileStore provides read access to uploaded files.
//
// Writers are intentionally separate. A service only needs to read a file to
// analyze or import it; HTTP and other upload paths can opt into FileWriter.
type FileStore interface {
	Open(context.Context, FileID) (io.ReadCloser, FileMetadata, error)
}

// FileWriter is the optional write side of a FileStore.
type FileWriter interface {
	Save(context.Context, FileMetadata, io.Reader) (FileMetadata, error)
}

// FileDeleter is an optional cleanup capability for stores that support it.
type FileDeleter interface {
	Delete(context.Context, FileID) error
}
