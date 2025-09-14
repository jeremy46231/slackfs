package fs

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// Minimal in-repo filesystem for testing: exposes a root directory
// with a single regular file `test.txt` whose content is "hello world\n".

type SlackFS struct {
	// future: Slack client reference
}

// Ensure Node embedding for go-fuse
var _ = (fs.InodeEmbedder)((*RootDir)(nil))

// RootDir is the root inode. Embed fs.Inode for convenience when creating children.
type RootDir struct {
	fs.Inode
}

func NewRoot() *RootDir {
	return &RootDir{}
}

// OnAdd is called when the inode is attached to the tree.
func (r *RootDir) OnAdd(ctx context.Context) {
	// no-op
}

// Getattr sets directory attributes.
func (r *RootDir) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = uint32(syscall.S_IFDIR | 0755)
	return 0
}

// Lookup finds a child by name. We support a single file `test.txt`.
func (r *RootDir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	switch name {
	case "test.txt":
		node := &HelloFile{}
		out.Mode = syscall.S_IFREG | 0444
		return r.NewInode(ctx, node, fs.StableAttr{Mode: uint32(fuse.S_IFREG)}), 0
	case "append.txt":
		node := &AppendFile{}
		out.Mode = syscall.S_IFREG | 0666
		return r.NewInode(ctx, node, fs.StableAttr{Mode: uint32(fuse.S_IFREG)}), 0
	case "stream.txt":
		node := &StreamFile{}
		out.Mode = syscall.S_IFREG | 0444
		return r.NewInode(ctx, node, fs.StableAttr{Mode: uint32(fuse.S_IFREG)}), 0
	default:
		return nil, syscall.ENOENT
	}
}

// Readdir lists the single entry `test.txt`.
func (r *RootDir) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	entries := []fuse.DirEntry{
		{Name: "test.txt", Mode: fuse.S_IFREG},
		{Name: "append.txt", Mode: fuse.S_IFREG},
		{Name: "stream.txt", Mode: fuse.S_IFREG},
	}
	return fs.NewListDirStream(entries), 0
}

// HelloFile is a regular file node returning "hello world\n".
type HelloFile struct {
	fs.Inode
}

var _ = (fs.NodeOpener)((*HelloFile)(nil))
var _ = (fs.NodeReader)((*HelloFile)(nil))

func (f *HelloFile) OnAdd(ctx context.Context) {}

func (f *HelloFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// allow read-only opens
	return nil, fuse.FOPEN_KEEP_CACHE, 0
}

// Getattr reports file attributes including size so tools like `cat` and `ls` see content.
func (f *HelloFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	data := []byte("hello world\n")
	out.Mode = uint32(syscall.S_IFREG | 0444)
	out.Size = uint64(len(data))
	return 0
}

func (f *HelloFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	data := []byte("hello world\n")
	if off >= int64(len(data)) {
		return fuse.ReadResultData(nil), 0
	}
	// compute slice to return
	end := int(off) + len(dest)
	if end > len(data) {
		end = len(data)
	}
	return fuse.ReadResultData(data[off:end]), 0
}

// AppendFile is a writable, append-only in-memory file.
type AppendFile struct {
	fs.Inode
	mu   sync.Mutex
	data []byte
}

var _ = (fs.NodeOpener)((*AppendFile)(nil))
var _ = (fs.NodeReader)((*AppendFile)(nil))
var _ = (fs.NodeWriter)((*AppendFile)(nil))

func (f *AppendFile) OnAdd(ctx context.Context) {}

func (f *AppendFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	f.mu.Lock()
	sz := uint64(len(f.data))
	f.mu.Unlock()
	out.Mode = uint32(syscall.S_IFREG | 0666)
	out.Size = sz
	return 0
}

func (f *AppendFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, fuse.FOPEN_KEEP_CACHE, 0
}

func (f *AppendFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	f.mu.Lock()
	data := append([]byte(nil), f.data...)
	f.mu.Unlock()
	if off >= int64(len(data)) {
		return fuse.ReadResultData(nil), 0
	}
	end := int(off) + len(dest)
	if end > len(data) {
		end = len(data)
	}
	return fuse.ReadResultData(data[off:end]), 0
}

func (f *AppendFile) Write(ctx context.Context, fh fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	f.mu.Lock()
	old := int64(len(f.data))
	f.data = append(f.data, data...)
	newLen := int64(len(f.data))
	f.mu.Unlock()
	// Notify kernel asynchronously so we don't block the FUSE request
	go func(off, sz int64, inode *fs.Inode) {
		// best-effort notification
		_ = inode.NotifyContent(off, sz)
	}(old, newLen-old, &f.Inode)
	return uint32(len(data)), 0
}

// StreamFile auto-appends a line every 2s. It's read-only from userspace.
type StreamFile struct {
	fs.Inode
	mu   sync.Mutex
	data []byte
}

var _ = (fs.NodeOpener)((*StreamFile)(nil))
var _ = (fs.NodeReader)((*StreamFile)(nil))

func (s *StreamFile) OnAdd(ctx context.Context) {
	// start background appender that stops when ctx is done
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				line := fmt.Sprintf("%s: heartbeat\n", t.Format(time.RFC3339))
				s.mu.Lock()
				off := int64(len(s.data))
				s.data = append(s.data, []byte(line)...)
				added := int64(len(line))
				s.mu.Unlock()
				// notify kernel about content change
				_ = s.Inode.NotifyContent(off, added)
			}
		}
	}()
}

func (s *StreamFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	s.mu.Lock()
	sz := uint64(len(s.data))
	s.mu.Unlock()
	out.Mode = uint32(syscall.S_IFREG | 0444)
	out.Size = sz
	return 0
}

func (s *StreamFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, fuse.FOPEN_KEEP_CACHE, 0
}

func (s *StreamFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	s.mu.Lock()
	data := append([]byte(nil), s.data...)
	s.mu.Unlock()
	if off >= int64(len(data)) {
		return fuse.ReadResultData(nil), 0
	}
	end := int(off) + len(dest)
	if end > len(data) {
		end = len(data)
	}
	return fuse.ReadResultData(data[off:end]), 0
}
