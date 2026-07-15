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

func TestReadClampsOversizedMaxBytes(t *testing.T) {
	a, dir := mkRoot(t)
	big := strings.Repeat("a", 5<<20)
	if err := os.WriteFile(filepath.Join(dir, "huge.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := a.Read("huge.txt", fileapi.ReadOptions{MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Bytes) != 4<<20 {
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

func TestWriteCreatesFile(t *testing.T) {
	a, dir := mkRoot(t)
	if _, err := a.Write("sub/new.txt", []byte("hello"), fileapi.WriteOptions{Truncate: true}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "sub", "new.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("read back: %q err=%v", got, err)
	}
}

func TestWriteChunkedByOffset(t *testing.T) {
	a, dir := mkRoot(t)
	// First chunk truncates+creates; later chunks append at their offset.
	if _, err := a.Write("blob.dat", []byte("abc"), fileapi.WriteOptions{Offset: 0, Truncate: true}); err != nil {
		t.Fatal(err)
	}
	size, err := a.Write("blob.dat", []byte("def"), fileapi.WriteOptions{Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	if size != 6 {
		t.Fatalf("size: got %d want 6", size)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "blob.dat"))
	if string(got) != "abcdef" {
		t.Fatalf("contents: %q", got)
	}
}

func TestWriteTruncateReplacesExisting(t *testing.T) {
	a, dir := mkRoot(t)
	// hello.txt starts as "hi there\n"; a truncating write must replace it.
	if _, err := a.Write("hello.txt", []byte("x"), fileapi.WriteOptions{Truncate: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if string(got) != "x" {
		t.Fatalf("contents: %q", got)
	}
}

func TestWriteRejectsDirectory(t *testing.T) {
	a, _ := mkRoot(t)
	if _, err := a.Write("sub", []byte("x"), fileapi.WriteOptions{Truncate: true}); err == nil {
		t.Fatal("expected error writing over a directory")
	}
}

func TestWriteEscapesRoot(t *testing.T) {
	a, _ := mkRoot(t)
	if _, err := a.Write("../escape.txt", []byte("x"), fileapi.WriteOptions{}); !errors.Is(err, fileapi.ErrEscapesRoot) {
		t.Fatalf("expected ErrEscapesRoot, got %v", err)
	}
}

func TestRemoveFileAndDir(t *testing.T) {
	a, dir := mkRoot(t)
	if err := a.Remove("hello.txt"); err != nil {
		t.Fatalf("Remove file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
	if err := a.Remove("sub"); err != nil {
		t.Fatalf("Remove dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); !os.IsNotExist(err) {
		t.Fatal("dir should be gone")
	}
}

func TestRemoveMissingErrors(t *testing.T) {
	a, _ := mkRoot(t)
	if err := a.Remove("nope.txt"); err == nil {
		t.Fatal("expected error removing missing file")
	}
}

func TestRemoveRefusesRoot(t *testing.T) {
	a, _ := mkRoot(t)
	for _, p := range []string{"", ".", "../"} {
		if err := a.Remove(p); !errors.Is(err, fileapi.ErrEscapesRoot) {
			t.Fatalf("Remove(%q): expected ErrEscapesRoot, got %v", p, err)
		}
	}
}

func TestMoveRenames(t *testing.T) {
	a, dir := mkRoot(t)
	if err := a.Move("hello.txt", "renamed.txt"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("original should be gone")
	}
}

func TestMoveIntoDirectory(t *testing.T) {
	a, dir := mkRoot(t)
	if err := a.Move("hello.txt", "sub/hello.txt"); err != nil {
		t.Fatalf("Move into dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "hello.txt")); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}
}

func TestMoveRefusesExistingDestination(t *testing.T) {
	a, _ := mkRoot(t)
	if err := a.Move("hello.txt", "blob.bin"); err == nil {
		t.Fatal("expected error overwriting existing destination")
	}
}

func TestMoveEscapesRoot(t *testing.T) {
	a, _ := mkRoot(t)
	if err := a.Move("hello.txt", "../escape.txt"); !errors.Is(err, fileapi.ErrEscapesRoot) {
		t.Fatalf("expected ErrEscapesRoot, got %v", err)
	}
}
