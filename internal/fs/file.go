package fs

import (
	"context"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// File is a reusable in-memory file. It is safe for concurrent readers/writers.
// Concrete file types can embed this and override behaviors if needed.
type File struct {
	fs.Inode
	mu      sync.RWMutex
	content []byte
}

func NewFile() *File { return &File{content: []byte{}} }

func (f *File) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	f.mu.RLock()
	sz := uint64(len(f.content))
	f.mu.RUnlock()
	out.Mode = uint32(syscall.S_IFREG | 0644)
	out.Size = sz
	now := time.Now()
	out.SetTimes(nil, &now, &now)
	// Set ownership to the mounting user so 0644 permits writes as expected.
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
	return 0
}

func (f *File) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	f.mu.RLock()
	// copy buffer to avoid data races when returning slice
	buf := make([]byte, len(f.content))
	copy(buf, f.content)
	f.mu.RUnlock()
	if off >= int64(len(buf)) {
		return fuse.ReadResultData(nil), 0
	}
	end := int(off) + len(dest)
	if end > len(buf) {
		end = len(buf)
	}
	return fuse.ReadResultData(buf[off:end]), 0
}

func (f *File) Write(ctx context.Context, fh fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if off < 0 {
		return 0, syscall.EINVAL
	}
	cur := int64(len(f.content))
	end := off + int64(len(data))
	if end > cur {
		// grow with zero padding
		pad := make([]byte, end-cur)
		f.content = append(f.content, pad...)
	}
	copy(f.content[off:end], data)
	// notify kernel that content changed
	_ = f.Inode.NotifyContent(off, int64(len(data)))
	return uint32(len(data)), 0
}

// Open defaults to read-only. Writable behavior must be explicitly opted-in by overriding Open.
func (f *File) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// Deny write attempts by default.
	if flags&syscall.O_WRONLY != 0 || flags&syscall.O_RDWR != 0 {
		return nil, 0, syscall.EACCES
	}
	// Allow read-only opens. Use DIRECT_IO for freshness.
	return nil, fuse.FOPEN_DIRECT_IO, 0
}
