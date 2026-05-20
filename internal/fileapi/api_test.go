package fileapi_test

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Mutech-Innovation/coding-anywhere/internal/fileapi"
)

func mkRoot(t *testing.T) (*fileapi.API, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Binary blob with NUL byte.
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatal(err)
	}
	api, err := fileapi.New(dir)
	if err != nil {
		t.Fatalf("fileapi.New: %v", err)
	}
	return api, dir
}

func TestListHidesDotEntriesByDefault(t *testing.T) {
	a, _ := mkRoot(t)
	es, err := a.List("", fileapi.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := []string{}
	for _, e := range es {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	if got, want := strings.Join(names, ","), "blob.bin,hello.txt,sub"; got != want {
		t.Fatalf("names: got %q want %q", got, want)
	}
}

func TestListIncludesHiddenWhenAsked(t *testing.T) {
	a, _ := mkRoot(t)
	es, err := a.List("", fileapi.ListOptions{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 4 {
		t.Fatalf("got %d entries, want 4", len(es))
	}
}

func TestReadTextFile(t *testing.T) {
	a, _ := mkRoot(t)
	res, err := a.Read("hello.txt", fileapi.ReadOptions{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.IsBinary {
		t.Fatal("expected text classification")
	}
	if string(res.Bytes) != "hi there\n" {
		t.Fatalf("bytes: %q", res.Bytes)
	}
	if res.Truncated {
		t.Fatal("not expected to truncate small file")
	}
}

func TestReadBinaryFileWithholdsBytes(t *testing.T) {
	a, _ := mkRoot(t)
	res, err := a.Read("blob.bin", fileapi.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsBinary {
		t.Fatal("expected binary classification")
	}
	if len(res.Bytes) != 0 {
		t.Fatalf("bytes returned for binary without AllowBinary: %d", len(res.Bytes))
	}
	res, err = a.Read("blob.bin", fileapi.ReadOptions{AllowBinary: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsBinary || len(res.Bytes) != 4 {
		t.Fatalf("unexpected: binary=%v len=%d", res.IsBinary, len(res.Bytes))
	}
}

func TestReadTruncatesAtMaxBytes(t *testing.T) {
	a, dir := mkRoot(t)
	big := strings.Repeat("a", 1024)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := a.Read("big.txt", fileapi.ReadOptions{MaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Bytes) != 100 {
		t.Fatalf("unexpected: truncated=%v len=%d", res.Truncated, len(res.Bytes))
	}
}

func TestReadOffsetAndLength(t *testing.T) {
	a, _ := mkRoot(t)
	res, err := a.Read("hello.txt", fileapi.ReadOptions{Offset: 3, Length: 5})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Bytes) != "there" {
		t.Fatalf("got %q", res.Bytes)
	}
}

func TestEscapesRoot(t *testing.T) {
	a, _ := mkRoot(t)
	_, err := a.List("../etc", fileapi.ListOptions{})
	if !errors.Is(err, fileapi.ErrEscapesRoot) {
		t.Fatalf("expected ErrEscapesRoot, got %v", err)
	}
}

func TestStatReturnsKind(t *testing.T) {
	a, _ := mkRoot(t)
	if e, err := a.Stat("hello.txt"); err != nil || e.Kind != fileapi.EntryFile {
		t.Fatalf("stat file: %+v err=%v", e, err)
	}
	if e, err := a.Stat("sub"); err != nil || e.Kind != fileapi.EntryDirectory {
		t.Fatalf("stat dir: %+v err=%v", e, err)
	}
}
