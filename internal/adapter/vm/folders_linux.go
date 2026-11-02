//go:build linux

package vm

import (
	"crypto/subtle"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/linux"
	"github.com/hugelgupf/p9/p9"
	"golang.org/x/sys/unix"
)

// QEMU cannot add a shared folder to a running machine, so on Linux this
// process serves the machine its folders over 9P, on the network it runs for
// the machine: UGT's own and the workspaces from boot, and each command's
// folder while the command runs. Two things keep a folder to the machine's
// kernel and to that folder:
//
//   - the network reaches the containers in the machine too, so a connection
//     is served only once its first line is the folder's secret, which ugt
//     hands the machine's root (QEMU's firmware configuration at boot, SSH
//     after). The machine sends it before it mounts the connection
//     (machine/mount9p), so it is never a mount option, which every
//     container can read in its own /proc/self/mountinfo;
//   - every path is resolved inside the folder by os.Root, so no link,
//     however a recipe makes it, reaches anything outside it.
//
// Files are read and written as the user running ugt, whatever user in the
// machine asks, as Docker Desktop's file sharing does.

// serveFolder serves dir on ln to each connection that first says secret,
// read-only or not, until the returned closer is closed.
func serveFolder(ln net.Listener, dir, secret string, readOnly bool) (io.Closer, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	server := p9.NewServer(&folder{root: root, readOnly: readOnly, qids: map[[2]uint64]uint64{}})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				if !said(conn, secret) {
					_ = conn.Close()
					return
				}
				_ = server.Handle(conn, conn)
			}()
		}
	}()
	return closers{ln, root}, nil
}

// said reads a connection's first line, within a few seconds, and reports
// whether it is secret.
func said(conn net.Conn, secret string) bool {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line := make([]byte, len(secret)+1)
	if _, err := io.ReadFull(conn, line); err != nil {
		return false
	}
	_ = conn.SetReadDeadline(time.Time{})
	return subtle.ConstantTimeCompare(line, []byte(secret+"\n")) == 1
}

type closers []io.Closer

func (c closers) Close() error {
	var errs []error
	for _, x := range c {
		errs = append(errs, x.Close())
	}
	return errors.Join(errs...)
}

// folder is one shared folder: what every node in it shares.
type folder struct {
	root     *os.Root
	readOnly bool

	mu   sync.Mutex
	qids map[[2]uint64]uint64 // a file's device and inode, to its 9P path
}

// Attach is the folder itself: a connection that reached it said the secret.
func (f *folder) Attach() (p9.File, error) { return &node{f: f, path: "."}, nil }

// qid names a file to the client, the same file always by the same path.
func (f *folder) qid(fi os.FileInfo) p9.QID {
	st, _ := fi.Sys().(*syscall.Stat_t)
	key := [2]uint64{}
	if st != nil {
		key = [2]uint64{uint64(st.Dev), st.Ino}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.qids[key]
	if !ok {
		q = uint64(len(f.qids) + 1)
		f.qids[key] = q
	}
	return p9.QID{Type: p9.ModeFromOS(fi.Mode()).QIDType(), Path: q}
}

// node is a file or folder in a shared folder, by its path inside it.
type node struct {
	p9.DefaultWalkGetAttr
	templatefs.XattrUnimplemented
	templatefs.NoopRenamed

	f    *folder
	path string   // relative to the folder; "." is the folder itself
	file *os.File // once opened
	dir  []os.DirEntry
}

func (n *node) child(name string) string { return path.Join(n.path, name) }

func (n *node) stat() (os.FileInfo, error) {
	if n.file != nil {
		return n.file.Stat()
	}
	return n.f.root.Lstat(n.path)
}

func (n *node) writable() error {
	if n.f.readOnly {
		return linux.EROFS
	}
	return nil
}

func (n *node) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, &node{f: n.f, path: n.path}, nil
	}
	var qids []p9.QID
	at := n.path
	for _, name := range names {
		at = path.Join(at, name)
		fi, err := n.f.root.Lstat(at)
		if err != nil {
			return nil, nil, err
		}
		qids = append(qids, n.f.qid(fi))
	}
	return qids, &node{f: n.f, path: at}, nil
}

func (n *node) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	fi, err := n.stat()
	if err != nil {
		return p9.QID{}, p9.AttrMask{}, p9.Attr{}, err
	}
	attr := p9.Attr{Mode: p9.ModeFromOS(fi.Mode()), Size: uint64(fi.Size()), NLink: 1}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		attr.UID, attr.GID, attr.NLink, attr.RDev = p9.UID(st.Uid), p9.GID(st.Gid), p9.NLink(st.Nlink), p9.Dev(st.Rdev)
		attr.BlockSize, attr.Blocks = uint64(st.Blksize), uint64(st.Blocks)
		attr.ATimeSeconds, attr.ATimeNanoSeconds = uint64(st.Atim.Sec), uint64(st.Atim.Nsec)
		attr.MTimeSeconds, attr.MTimeNanoSeconds = uint64(st.Mtim.Sec), uint64(st.Mtim.Nsec)
		attr.CTimeSeconds, attr.CTimeNanoSeconds = uint64(st.Ctim.Sec), uint64(st.Ctim.Nsec)
	}
	return n.f.qid(fi), req, attr, nil
}

// openFlags are the open flags a client may ask for, of those 9P carries.
const openFlags = unix.O_ACCMODE | unix.O_TRUNC | unix.O_APPEND | unix.O_SYNC | unix.O_DSYNC

func (n *node) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	flags := int(mode) & openFlags
	if flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_TRUNC != 0 {
		if err := n.writable(); err != nil {
			return p9.QID{}, 0, err
		}
	}
	f, err := n.f.root.OpenFile(n.path, flags|unix.O_NOFOLLOW, 0)
	if err != nil {
		return p9.QID{}, 0, err
	}
	n.file = f
	qid, _, _, err := n.GetAttr(p9.AttrMask{})
	return qid, 0, err
}

func (n *node) ReadAt(p []byte, offset int64) (int, error) {
	if n.file == nil {
		return 0, linux.EBADF
	}
	got, err := n.file.ReadAt(p, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return got, err
}

func (n *node) WriteAt(p []byte, offset int64) (int, error) {
	if n.file == nil {
		return 0, linux.EBADF
	}
	return n.file.WriteAt(p, offset)
}

func (n *node) FSync() error {
	if n.file == nil {
		return nil
	}
	return n.file.Sync()
}

func (n *node) Close() error {
	if n.file == nil {
		return nil
	}
	return n.file.Close()
}

// Readdir lists the folder once, when it is first read, and pages through
// that listing: offsets are positions in it.
func (n *node) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	if n.file == nil {
		return nil, linux.EBADF
	}
	if offset == 0 || n.dir == nil {
		entries, err := fs.ReadDir(n.f.root.FS(), n.path)
		if err != nil {
			return nil, err
		}
		n.dir = entries
	}
	var out p9.Dirents
	for i := offset; i < uint64(len(n.dir)) && len(out) < int(count); i++ {
		e := n.dir[i]
		fi, err := e.Info()
		if err != nil {
			continue // removed since it was listed
		}
		qid := n.f.qid(fi)
		out = append(out, p9.Dirent{QID: qid, Type: qid.Type, Name: e.Name(), Offset: i + 1})
	}
	return out, nil
}

func (n *node) Create(name string, mode p9.OpenFlags, perm p9.FileMode, _ p9.UID, _ p9.GID) (p9.File, p9.QID, uint32, error) {
	if err := n.writable(); err != nil {
		return nil, p9.QID{}, 0, err
	}
	created := &node{f: n.f, path: n.child(name)}
	f, err := n.f.root.OpenFile(created.path, int(mode)&openFlags|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, os.FileMode(perm.Permissions()))
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	created.file = f
	qid, _, _, err := created.GetAttr(p9.AttrMask{})
	if err != nil {
		_ = f.Close()
		return nil, p9.QID{}, 0, err
	}
	return created, qid, 0, nil
}

func (n *node) made(name string) (p9.QID, error) {
	fi, err := n.f.root.Lstat(n.child(name))
	if err != nil {
		return p9.QID{}, err
	}
	return n.f.qid(fi), nil
}

func (n *node) Mkdir(name string, perm p9.FileMode, _ p9.UID, _ p9.GID) (p9.QID, error) {
	if err := n.writable(); err != nil {
		return p9.QID{}, err
	}
	if err := n.f.root.Mkdir(n.child(name), os.FileMode(perm.Permissions())); err != nil {
		return p9.QID{}, err
	}
	return n.made(name)
}

// Symlink makes a link whatever it names: the machine follows it in the
// machine, and this side never follows it out of the folder.
func (n *node) Symlink(target, name string, _ p9.UID, _ p9.GID) (p9.QID, error) {
	if err := n.writable(); err != nil {
		return p9.QID{}, err
	}
	if err := n.f.root.Symlink(target, n.child(name)); err != nil {
		return p9.QID{}, err
	}
	return n.made(name)
}

func (n *node) Link(target p9.File, name string) error {
	if err := n.writable(); err != nil {
		return err
	}
	t, ok := target.(*node)
	if !ok || t.f != n.f {
		return linux.EXDEV
	}
	return n.f.root.Link(t.path, n.child(name))
}

func (n *node) Mknod(string, p9.FileMode, uint32, uint32, p9.UID, p9.GID) (p9.QID, error) {
	return p9.QID{}, linux.EPERM // no devices or pipes in the user's folders
}

func (n *node) RenameAt(oldName string, newDir p9.File, newName string) error {
	if err := n.writable(); err != nil {
		return err
	}
	to, ok := newDir.(*node)
	if !ok || to.f != n.f {
		return linux.EXDEV
	}
	return n.f.root.Rename(n.child(oldName), to.child(newName))
}

func (n *node) Renamed(parent p9.File, name string) {
	if p, ok := parent.(*node); ok {
		n.path = p.child(name)
	}
}

func (n *node) Rename(p9.File, string) error { return linux.ENOSYS } // 9P2000.L renames with RenameAt
func (n *node) Remove() error                { return linux.ENOSYS } // and removes with UnlinkAt

func (n *node) UnlinkAt(name string, _ uint32) error {
	if err := n.writable(); err != nil {
		return err
	}
	return n.f.root.Remove(n.child(name))
}

func (n *node) Readlink() (string, error) { return n.f.root.Readlink(n.path) }

// SetAttr changes a file's size, mode and times. Its owner stays the user's:
// a change of owner is accepted and does nothing, as the user cannot give a
// file away.
func (n *node) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	if err := n.writable(); err != nil {
		return err
	}
	if valid.Size {
		f := n.file
		if f == nil {
			var err error
			if f, err = n.f.root.OpenFile(n.path, os.O_WRONLY|unix.O_NOFOLLOW, 0); err != nil {
				return err
			}
			defer f.Close()
		}
		if err := f.Truncate(int64(attr.Size)); err != nil {
			return err
		}
	}
	if valid.Permissions {
		if err := n.f.root.Chmod(n.path, os.FileMode(attr.Permissions.Permissions())); err != nil {
			return err
		}
	}
	if valid.ATime || valid.MTime {
		fi, err := n.stat()
		if err != nil {
			return err
		}
		now := time.Now()
		atime, mtime := now, now
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			atime = time.Unix(st.Atim.Sec, st.Atim.Nsec)
		}
		mtime = fi.ModTime()
		if valid.ATime {
			atime = now
			if valid.ATimeNotSystemTime {
				atime = time.Unix(int64(attr.ATimeSeconds), int64(attr.ATimeNanoSeconds))
			}
		}
		if valid.MTime {
			mtime = now
			if valid.MTimeNotSystemTime {
				mtime = time.Unix(int64(attr.MTimeSeconds), int64(attr.MTimeNanoSeconds))
			}
		}
		if err := n.f.root.Chtimes(n.path, atime, mtime); err != nil {
			return err
		}
	}
	return nil
}

func (n *node) StatFS() (p9.FSStat, error) {
	d, err := n.f.root.Open(".")
	if err != nil {
		return p9.FSStat{}, err
	}
	defer d.Close()
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(d.Fd()), &st); err != nil {
		return p9.FSStat{}, err
	}
	return p9.FSStat{Type: 0x01021997, BlockSize: uint32(st.Bsize), Blocks: st.Blocks, BlocksFree: st.Bfree,
		BlocksAvailable: st.Bavail, Files: st.Files, FilesFree: st.Ffree, NameLength: 255}, nil
}

// Lock grants every lock: the folder has one client, the machine's kernel,
// which keeps its own processes' locks apart.
func (n *node) Lock(int, p9.LockType, p9.LockFlags, uint64, uint64, string) (p9.LockStatus, error) {
	return p9.LockStatusOK, nil
}
