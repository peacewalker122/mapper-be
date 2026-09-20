package tempfile

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/peacewalker122/mapper/mapper"
)

func TestStoreRoundTrip(t *testing.T) {
	store := New(t.TempDir())
	want := "name,email\nAda,ada@example.com\n"

	metadata, err := store.Save(context.Background(), mapper.FileMetadata{Name: "users.csv"}, strings.NewReader(want))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !metadata.ID.Valid() || metadata.Size != int64(len(want)) {
		t.Fatalf("saved metadata = %#v", metadata)
	}

	reader, opened, err := store.Open(context.Background(), metadata.ID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(got) != want || opened != metadata {
		t.Fatalf("opened file = %q, metadata = %#v", got, opened)
	}

	if err := store.Delete(context.Background(), metadata.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, _, err := store.Open(context.Background(), metadata.ID); err == nil {
		t.Fatal("Open() after Delete() succeeded")
	}
}
