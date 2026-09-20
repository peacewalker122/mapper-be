// Package tempfile stores uploaded files in a local directory.
package tempfile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/peacewalker122/mapper/mapper"
)

var (
	ErrInvalidFileID = errors.New("invalid file id")
	ErrNilReader     = errors.New("nil file reader")
	ErrStoreClosed   = errors.New("file store closed")
)

// Store is a local FileStore backed by one directory per process. Files are
// written atomically and IDs are random, opaque file names.
type Store struct {
	root    string
	owned   bool
	initErr error

	mu       sync.RWMutex
	metadata map[mapper.FileID]mapper.FileMetadata
	closed   bool
}

// FileStore is kept as an alias for callers that name implementations after
// the interface they satisfy.
type FileStore = Store

// New creates a Store. With no directory, it creates an owned temporary
// directory; with one directory, it uses that directory and leaves cleanup to
// the caller.
func New(directory ...string) *Store {
	root := ""
	if len(directory) > 0 {
		root = directory[0]
	}

	store := &Store{root: root, metadata: make(map[mapper.FileID]mapper.FileMetadata)}
	if root == "" {
		store.root, store.initErr = os.MkdirTemp("", "mapper-files-")
		store.owned = store.initErr == nil
	} else {
		store.initErr = os.MkdirAll(root, 0o700)
	}
	return store
}

// NewWithError is the error-returning form of New for callers that need to
// fail construction when the directory cannot be created.
func NewWithError(directory ...string) (*Store, error) {
	store := New(directory...)
	if store.initErr != nil {
		return nil, store.initErr
	}
	return store, nil
}

// NewStore is an explicit constructor alias.
func NewStore(directory ...string) *Store { return New(directory...) }

// NewTempFileStore is an explicit constructor alias.
func NewTempFileStore(directory ...string) *Store { return New(directory...) }

// Dir returns the directory used by the store.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Root returns the directory used by the store.
func (s *Store) Root() string { return s.Dir() }

// Save stores src under a newly generated FileID and returns completed
// metadata. Caller-provided IDs are never used as paths.
func (s *Store) Save(ctx context.Context, metadata mapper.FileMetadata, src io.Reader) (mapper.FileMetadata, error) {
	if s == nil {
		return mapper.FileMetadata{}, ErrStoreClosed
	}
	if src == nil {
		return mapper.FileMetadata{}, ErrNilReader
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return mapper.FileMetadata{}, err
	}
	if err := s.ready(); err != nil {
		return mapper.FileMetadata{}, err
	}

	for attempt := 0; attempt < 8; attempt++ {
		id, err := newFileID()
		if err != nil {
			return mapper.FileMetadata{}, err
		}
		path, err := s.filePath(id)
		if err != nil {
			return mapper.FileMetadata{}, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return mapper.FileMetadata{}, fmt.Errorf("create file: %w", err)
		}

		n, copyErr := io.Copy(file, contextReader{ctx: ctx, reader: src})
		closeErr := file.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr == nil {
			copyErr = ctx.Err()
		}
		if copyErr != nil {
			_ = os.Remove(path)
			return mapper.FileMetadata{}, copyErr
		}

		saved := metadata
		saved.ID = id
		saved.Size = n
		saved.CreatedAt = time.Now().UTC()

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = os.Remove(path)
			return mapper.FileMetadata{}, ErrStoreClosed
		}
		s.metadata[id] = saved
		s.mu.Unlock()
		return saved, nil
	}
	return mapper.FileMetadata{}, errors.New("could not allocate unique file id")
}

// Open opens a completed file and returns its stored metadata.
func (s *Store) Open(ctx context.Context, id mapper.FileID) (io.ReadCloser, mapper.FileMetadata, error) {
	if s == nil {
		return nil, mapper.FileMetadata{}, ErrStoreClosed
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, mapper.FileMetadata{}, err
	}
	if err := s.ready(); err != nil {
		return nil, mapper.FileMetadata{}, err
	}
	path, err := s.filePath(id)
	if err != nil {
		return nil, mapper.FileMetadata{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, mapper.FileMetadata{}, fmt.Errorf("open file %q: %w", id, err)
	}

	metadata, ok := s.lookup(id)
	if !ok {
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return nil, mapper.FileMetadata{}, fmt.Errorf("stat file %q: %w", id, statErr)
		}
		metadata = mapper.FileMetadata{ID: id, Name: string(id), Size: info.Size()}
	}
	return file, metadata, nil
}

// Delete removes a completed file.
func (s *Store) Delete(ctx context.Context, id mapper.FileID) error {
	if s == nil {
		return ErrStoreClosed
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	path, err := s.filePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete file %q: %w", id, err)
	}
	s.mu.Lock()
	delete(s.metadata, id)
	s.mu.Unlock()
	return nil
}

// Close removes an automatically-created temporary directory. Stores using a
// caller-provided directory remain intact.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	if !s.owned {
		return nil
	}
	return os.RemoveAll(s.root)
}

// Cleanup is an alias for Close.
func (s *Store) Cleanup() error { return s.Close() }

func (s *Store) ready() error {
	s.mu.RLock()
	closed := s.closed
	initErr := s.initErr
	s.mu.RUnlock()
	if closed {
		return ErrStoreClosed
	}
	if initErr != nil {
		return fmt.Errorf("initialize file store: %w", initErr)
	}
	return nil
}

func (s *Store) lookup(id mapper.FileID) (mapper.FileMetadata, bool) {
	s.mu.RLock()
	metadata, ok := s.metadata[id]
	s.mu.RUnlock()
	return metadata, ok
}

func (s *Store) filePath(id mapper.FileID) (string, error) {
	value := string(id)
	if value == "" || value == "." || value == ".." ||
		filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return "", fmt.Errorf("%w: %q", ErrInvalidFileID, id)
	}
	return filepath.Join(s.root, value), nil
}

func newFileID() (mapper.FileID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate file id: %w", err)
	}
	return mapper.FileID(hex.EncodeToString(raw[:])), nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err == nil {
		err = r.ctx.Err()
	}
	return n, err
}

var (
	_ mapper.FileStore   = (*Store)(nil)
	_ mapper.FileWriter  = (*Store)(nil)
	_ mapper.FileDeleter = (*Store)(nil)
)
