//go:build linux

package vm

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hugelgupf/p9/linux"
	"github.com/hugelgupf/p9/p9"
	"golang.org/x/sys/unix"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// folderClient serves dir and connects to it as the machine does: its first
// line, then 9P.
func folderClient(t *testing.T, dir, first string, readOnly bool) (p9.File, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served, err := serveFolder(ln, dir, testSecret, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = served.Close() })
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(first + "\n")); err != nil {
		t.Fatal(err)
	}
	c, err := p9.NewClient(conn)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = c.Close() })
	return c.Attach("")
}

func read(t *testing.T, root p9.File, names ...string) (string, error) {
	t.Helper()
	_, f, err := root.Walk(names)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, _, err := f.Open(p9.ReadOnly); err != nil {
		return "", err
	}
	buf := make([]byte, 100)
	n, err := f.ReadAt(buf, 0)
	return string(buf[:n]), err
}

// A folder answers only a connection whose first line is its secret: one
// that says another is cut off before any 9P.
func TestFolderAnswersOnlyItsSecret(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "note"), []byte("mine"), 0o600)
	if _, err := folderClient(t, dir, strings.Repeat("0", len(testSecret)), false); err == nil {
		t.Error("a connection that said another secret was served")
	}
	root, err := folderClient(t, dir, testSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := read(t, root, "note"); err != nil || got != "mine" {
		t.Errorf("with the secret: %q, %v", got, err)
	}
}

// No link reaches outside the folder, whether the machine made it or it was
// there already, and no path climbs out of it.
func TestFolderKeepsToItself(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("not yours"), 0o600)
	dir := t.TempDir()
	os.Symlink(outside, filepath.Join(dir, "planted"))
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "planted-file"))
	root, err := folderClient(t, dir, testSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Symlink(outside, "made", 0, 0); err != nil {
		t.Fatal(err) // a link may name anything; following it is what is refused
	}
	for _, path := range [][]string{{"planted", "secret"}, {"made", "secret"}, {"planted-file"}, {"..", filepath.Base(outside), "secret"}} {
		if got, err := read(t, root, path...); err == nil {
			t.Errorf("%v read %q from outside the folder", path, got)
		}
	}
	if _, f, err := root.Walk(nil); err == nil {
		if _, _, _, err := f.Create("../escaped", p9.WriteOnly, 0o644, 0, 0); err == nil {
			t.Error("made a file outside the folder")
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); err == nil {
		t.Error("a file appeared beside the folder")
	}
}

// A read-only folder refuses every change.
func TestFolderReadOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "key"), []byte("k"), 0o600)
	root, err := folderClient(t, dir, testSecret, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := read(t, root, "key"); err != nil || got != "k" {
		t.Fatalf("read %q, %v", got, err)
	}
	_, f, _ := root.Walk([]string{"key"})
	if _, _, err := f.Open(p9.WriteOnly); !errors.Is(err, linux.EROFS) {
		t.Errorf("opened for writing: %v", err)
	}
	_, d, _ := root.Walk(nil)
	if _, _, _, err := d.Create("new", p9.WriteOnly, 0o600, 0, 0); !errors.Is(err, linux.EROFS) {
		t.Errorf("created: %v", err)
	}
	if err := d.UnlinkAt("key", 0); !errors.Is(err, linux.EROFS) {
		t.Errorf("removed: %v", err)
	}
}

// What a recipe does in a folder: make, write, list, rename, remove.
func TestFolderReadsAndWrites(t *testing.T) {
	dir := t.TempDir()
	root, err := folderClient(t, dir, testSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	_, d, _ := root.Walk(nil)
	if _, err := d.Mkdir("out", 0o755, 0, 0); err != nil {
		t.Fatal(err)
	}
	_, out, _ := root.Walk([]string{"out"})
	f, _, _, err := out.Create("result.txt", p9.WriteOnly, 0o644, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if data, err := os.ReadFile(filepath.Join(dir, "out", "result.txt")); err != nil || string(data) != "hello" {
		t.Errorf("on disk: %q, %v", data, err)
	}
	// creating made out's handle the new file's, as 9P does: walk to it again
	_, out, _ = root.Walk([]string{"out"})
	if err := out.RenameAt("result.txt", out, "done.txt"); err != nil {
		t.Fatal(err)
	}
	_, list, err := root.Walk([]string{"out"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if _, _, err := list.Open(p9.ReadOnly); err != nil {
		t.Fatalf("open: %v", err)
	}
	entries, err := list.Readdir(0, 4096) // bytes, as the kernel asks
	if err != nil || len(entries) != 1 || entries[0].Name != "done.txt" {
		t.Errorf("listed %v, %v", entries, err)
	}
	if err := out.UnlinkAt("done.txt", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out", "done.txt")); !os.IsNotExist(err) {
		t.Errorf("still there: %v", err)
	}
}

// Taking a folder back ends every connection to it, and the files they had
// open: nothing it served stays readable.
func TestFolderReleaseEndsItsConnections(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "note"), []byte("mine"), 0o600)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served, err := serveFolder(ln, dir, testSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := net.Dial("tcp", ln.Addr().String())
	conn.Write([]byte(testSecret + "\n"))
	c, err := p9.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root, _ := c.Attach("")
	_, f, _ := root.Walk([]string{"note"})
	if _, _, err := f.Open(p9.ReadOnly); err != nil {
		t.Fatal(err)
	}
	served.Close()
	if n, err := f.ReadAt(make([]byte, 4), 0); err == nil {
		t.Errorf("read %d bytes after the folder was taken back", n)
	}
}

// A link's times change on the link, never on what it points at, and a link
// has no mode to change: a link in one recipe's workspace may point into
// another's.
func TestFolderMetadataStaysOnLinks(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "other-workspace", "result")
	os.MkdirAll(filepath.Dir(victim), 0o755)
	os.WriteFile(victim, []byte("theirs"), 0o644)
	was := time.Unix(1_700_000_000, 0)
	os.Chtimes(victim, was, was)
	os.MkdirAll(filepath.Join(dir, "mine"), 0o755)
	os.Symlink("../other-workspace/result", filepath.Join(dir, "mine", "link"))
	root, err := folderClient(t, dir, testSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	_, link, err := root.Walk([]string{"mine", "link"})
	if err != nil {
		t.Fatal(err)
	}
	at := p9.SetAttr{MTimeSeconds: 1000, ATimeSeconds: 1000}
	if err := link.SetAttr(p9.SetAttrMask{MTime: true, MTimeNotSystemTime: true, ATime: true, ATimeNotSystemTime: true}, at); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(victim); !fi.ModTime().Equal(was) {
		t.Errorf("the link's target was touched: %v", fi.ModTime())
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "mine", "link")); fi.ModTime().Unix() != 1000 {
		t.Errorf("the link itself: %v", fi.ModTime())
	}
	if err := link.SetAttr(p9.SetAttrMask{Permissions: true}, p9.SetAttr{Permissions: 0o777}); err == nil {
		t.Error("changed a link's mode")
	}
	if fi, _ := os.Stat(victim); fi.Mode().Perm() != 0o644 {
		t.Errorf("the link's target's mode: %v", fi.Mode())
	}
}

// What a recipe appends goes at the end, as `>>` in a shell does.
func TestFolderAppends(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "log"), []byte("a"), 0o644)
	root, err := folderClient(t, dir, testSecret, false)
	if err != nil {
		t.Fatal(err)
	}
	_, f, _ := root.Walk([]string{"log"})
	if _, _, err := f.Open(p9.OpenFlags(unix.O_WRONLY | unix.O_APPEND)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "log")); string(data) != "ab" {
		t.Errorf("log: %q", data)
	}
}
