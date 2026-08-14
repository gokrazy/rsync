package receiver

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gokrazy/rsync/internal/log"
)

func newTestTransfer(t *testing.T, dest string) *Transfer {
	t.Helper()
	root, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &Transfer{
		Logger:   log.New(io.Discard),
		Dest:     dest,
		DestRoot: root,
	}
}

func writeN(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0644); err != nil {
		t.Fatal(err)
	}
}

func sizeOf(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// TestRetainPartialKeepsLonger checks the basis only advances: a longer temp
// replaces the partial, a shorter one is dropped, and a missing temp is a no-op.
func TestRetainPartialKeepsLonger(t *testing.T) {
	const tmpName, partialName = "f.tgz.partial.tmp", "f.tgz.partial"

	t.Run("longer temp replaces partial", func(t *testing.T) {
		dir := t.TempDir()
		rt := newTestTransfer(t, dir)
		writeN(t, filepath.Join(dir, partialName), 100)
		writeN(t, filepath.Join(dir, tmpName), 250)

		rt.retainPartial(tmpName, partialName)

		if _, err := os.Stat(filepath.Join(dir, tmpName)); !os.IsNotExist(err) {
			t.Errorf("temp should be renamed away, stat err = %v", err)
		}
		if got := sizeOf(t, filepath.Join(dir, partialName)); got != 250 {
			t.Errorf("partial size = %d, want 250 (longer temp)", got)
		}
	})

	t.Run("shorter temp is discarded", func(t *testing.T) {
		dir := t.TempDir()
		rt := newTestTransfer(t, dir)
		writeN(t, filepath.Join(dir, partialName), 500)
		writeN(t, filepath.Join(dir, tmpName), 120)

		rt.retainPartial(tmpName, partialName)

		if _, err := os.Stat(filepath.Join(dir, tmpName)); !os.IsNotExist(err) {
			t.Errorf("shorter temp should be removed, stat err = %v", err)
		}
		if got := sizeOf(t, filepath.Join(dir, partialName)); got != 500 {
			t.Errorf("partial size = %d, want 500 (kept more complete partial)", got)
		}
	})

	t.Run("missing temp leaves partial intact", func(t *testing.T) {
		dir := t.TempDir()
		rt := newTestTransfer(t, dir)
		writeN(t, filepath.Join(dir, partialName), 300)

		rt.retainPartial(tmpName, partialName)

		if got := sizeOf(t, filepath.Join(dir, partialName)); got != 300 {
			t.Errorf("partial size = %d, want 300 (unchanged)", got)
		}
	})
}

// TestOpenPartialBasis checks a regular non-empty partial is opened, while a
// missing or empty one reports ok=false.
func TestOpenPartialBasis(t *testing.T) {
	dir := t.TempDir()
	rt := newTestTransfer(t, dir)

	if _, _, ok := rt.openPartialBasis(&File{Name: "missing.tgz"}); ok {
		t.Error("absent partial should report ok=false")
	}

	writeN(t, filepath.Join(dir, "empty.tgz"+partialSuffix), 0)
	if _, _, ok := rt.openPartialBasis(&File{Name: "empty.tgz"}); ok {
		t.Error("empty partial should report ok=false")
	}

	writeN(t, filepath.Join(dir, "good.tgz"+partialSuffix), 4096)
	in, sz, ok := rt.openPartialBasis(&File{Name: "good.tgz"})
	if !ok {
		t.Fatal("regular non-empty partial should report ok=true")
	}
	defer in.Close()
	if sz != 4096 {
		t.Errorf("basis size = %d, want 4096", sz)
	}
}
