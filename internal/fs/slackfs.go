package fs

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	sl "github.com/jeremy46231/slackfs/internal/slack"
)

// Minimal in-repo filesystem for testing: exposes a root directory
// with three files:
// - test.txt    (read-only)    → returns "hello world\n"
// - append.txt  (read/write)   → in-memory buffer; supports random writes; open with O_APPEND to force append-at-end
// - stream.txt  (read-only)    → background appends a line every 2s
//
// Note: append-only behavior for append.txt is not enforced. To make it strictly
// append-only, reject writes unless the file handle was opened with O_APPEND
// (but this doesn't seem to work on macOS, so I don't enforce it rn)

type SlackFS struct {
	// future: Slack client reference
}

// Ensure Node embedding for go-fuse
var _ = (fs.InodeEmbedder)((*RootDir)(nil))

// RootDir is the root inode. Embed fs.Inode for convenience when creating children.
type RootDir struct {
	fs.Inode
	nextIno uint64
	client  *sl.Client
}

func NewRoot(client *sl.Client) *RootDir {
	// Create root with proper stable attributes
	root := &RootDir{nextIno: 2, client: client} // reserve 1 for root
	// The embedded fs.Inode will be properly initialized by the mount process
	return root
}

// OnAdd is called when the inode is attached to the tree.
func (r *RootDir) OnAdd(ctx context.Context) {
	alloc := func() uint64 {
		if r.nextIno < 2 {
			r.nextIno = 2
		}
		ino := r.nextIno
		r.nextIno++
		return ino
	}

	// channels/ directory (dynamic)
	channels := NewChannelsDir(r.client)
	chInode := r.NewPersistentInode(ctx, channels, fs.StableAttr{Mode: uint32(fuse.S_IFDIR), Ino: alloc()})
	r.AddChild("channels", chInode, true)

	// test/ directory: move demo files here
	testDir := &Directory{}
	testIn := r.NewPersistentInode(ctx, testDir, fs.StableAttr{Mode: uint32(fuse.S_IFDIR), Ino: alloc()})
	r.AddChild("test", testIn, true)

	// Populate test directory contents
	hello := &HelloFile{}
	helloIn := testDir.NewPersistentInode(ctx, hello, fs.StableAttr{Mode: uint32(fuse.S_IFREG), Ino: alloc()})
	testDir.AddChild("test.txt", helloIn, true)

	appendNode := &AppendFile{}
	appendIn := testDir.NewPersistentInode(ctx, appendNode, fs.StableAttr{Mode: uint32(fuse.S_IFREG), Ino: alloc()})
	testDir.AddChild("append.txt", appendIn, true)

	stream := &StreamFile{}
	streamIn := testDir.NewPersistentInode(ctx, stream, fs.StableAttr{Mode: uint32(fuse.S_IFREG), Ino: alloc()})
	testDir.AddChild("stream.txt", streamIn, true)
}

// Getattr sets directory attributes.
func (r *RootDir) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = uint32(syscall.S_IFDIR | 0755)
	out.Ino = 1 // root directory has inode 1
	now := time.Now()
	out.SetTimes(nil, &now, &now) // atime, mtime, ctime
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
	return 0
}

// Lookup finds a child by name. We support a single file `test.txt`.
func (r *RootDir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	ch := r.GetChild(name)
	if ch == nil {
		return nil, syscall.ENOENT
	}
	// Mode will be returned by child Getattr; set a sensible default
	if ch.IsDir() {
		out.Mode = syscall.S_IFDIR | 0755
	} else {
		out.Mode = syscall.S_IFREG | 0644
	}
	return ch, 0
}

// Readdir lists directory entries with proper file types and offsets.

// Statfs provides basic filesystem stats to satisfy macOS expectations and tools like df.
var _ = (fs.NodeStatfser)((*RootDir)(nil))

func (r *RootDir) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	out.Blocks = 0
	out.Bsize = 4096
	out.NameLen = 255
	out.Files = 0
	out.Ffree = 0
	return 0
}

// HelloFile is a regular file node returning "hello world\n".
type HelloFile struct {
	fs.Inode
}

var _ = (fs.NodeOpener)((*HelloFile)(nil))
var _ = (fs.NodeReader)((*HelloFile)(nil))

func (f *HelloFile) OnAdd(ctx context.Context) {}

func (f *HelloFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// Only allow read access for this read-only file
	if flags&syscall.O_WRONLY != 0 || flags&syscall.O_RDWR != 0 {
		return nil, 0, syscall.EACCES
	}
	return nil, fuse.FOPEN_KEEP_CACHE, 0
}

// Getattr reports file attributes including size so tools like `cat` and `ls` see content.
func (f *HelloFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	data := []byte("hello world\n")
	out.Mode = uint32(syscall.S_IFREG | 0444)
	out.Size = uint64(len(data))
	now := time.Now()
	out.SetTimes(nil, &now, &now)
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
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
var _ = (fs.NodeSetattrer)((*AppendFile)(nil))

// appendFH tracks per-open flags like O_APPEND
type appendFH struct {
	append bool
}

func (f *AppendFile) OnAdd(ctx context.Context) {}

func (f *AppendFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	f.mu.Lock()
	sz := uint64(len(f.data))
	f.mu.Unlock()
	out.Mode = uint32(syscall.S_IFREG | 0666)
	out.Size = sz
	now := time.Now()
	out.SetTimes(nil, &now, &now)
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
	return 0
}

func (f *AppendFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// Handle open flags properly for append semantics
	if flags&syscall.O_WRONLY != 0 || flags&syscall.O_RDWR != 0 {
		// Writing is allowed
		if flags&syscall.O_TRUNC != 0 {
			// Truncate file on open
			f.mu.Lock()
			f.data = f.data[:0]
			f.mu.Unlock()
		}
	}
	fh := &appendFH{append: flags&syscall.O_APPEND != 0}
	// Use DIRECT_IO to avoid kernel page cache duplicating content for writes.
	return fh, fuse.FOPEN_DIRECT_IO, 0
}

func (f *AppendFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	f.mu.Lock()
	// Make a copy to avoid races while we're outside the lock
	data := make([]byte, len(f.data))
	copy(data, f.data)
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
	defer f.mu.Unlock()

	// Honor O_APPEND if set on this handle
	if h, ok := fh.(*appendFH); ok && h.append {
		off = int64(len(f.data))
	}

	// Ensure capacity up to off
	if off < 0 {
		return 0, syscall.EINVAL
	}
	curLen := int64(len(f.data))
	end := off + int64(len(data))
	if end > curLen {
		// grow slice to new size, padding with zeros if there is a hole
		padding := make([]byte, end-curLen)
		f.data = append(f.data, padding...)
	}
	copy(f.data[off:end], data)

	// Notify kernel about the modified region
	inode := &f.Inode
	sz := int64(len(data))
	go func(off, sz int64) {
		if errno := inode.NotifyContent(off, sz); errno != 0 {
			fmt.Printf("NotifyContent failed: %v\n", errno)
		}
		// There is no explicit InvalidateAttr API; rely on attr timeouts from mount options
		// and the next Getattr call to refresh size. NotifyContent helps flush page cache.
	}(off, sz)

	return uint32(len(data)), 0
}

func (f *AppendFile) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	// Support truncate via size change
	if sz, ok := in.GetSize(); ok {
		f.mu.Lock()
		cur := uint64(len(f.data))
		if sz < cur {
			f.data = f.data[:sz]
		} else if sz > cur {
			f.data = append(f.data, make([]byte, sz-cur)...)
		}
		f.mu.Unlock()

		out.Mode = uint32(syscall.S_IFREG | 0666)
		out.Size = uint64(len(f.data))
		return 0
	}
	return 0
}

// StreamFile auto-appends a line every 2s. It's read-only from userspace.
type StreamFile struct {
	fs.Inode
	mu   sync.Mutex
	data []byte
	// track active readers to avoid NotifyContent spam when nobody is watching
	readers int
}

var _ = (fs.NodeOpener)((*StreamFile)(nil))
var _ = (fs.NodeReader)((*StreamFile)(nil))
var _ = (fs.NodeReleaser)((*StreamFile)(nil))

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
				// notify kernel about content change only if there are active readers
				s.mu.Lock()
				readers := s.readers
				s.mu.Unlock()
				if readers > 0 {
					if errno := s.Inode.NotifyContent(off, added); errno != 0 {
						fmt.Printf("StreamFile NotifyContent failed: %v\n", errno)
					}
				}
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
	now := time.Now()
	out.SetTimes(nil, &now, &now)
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
	return 0
}

func (s *StreamFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	// Only allow read access for this read-only file
	if flags&syscall.O_WRONLY != 0 || flags&syscall.O_RDWR != 0 {
		return nil, 0, syscall.EACCES
	}
	// Count a reader for this open
	s.mu.Lock()
	s.readers++
	s.mu.Unlock()
	// Stream changes frequently; prefer DIRECT_IO for fresh reads.
	return nil, fuse.FOPEN_DIRECT_IO, 0
}

func (s *StreamFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	s.mu.Lock()
	// Make a copy to avoid races while we're outside the lock
	data := make([]byte, len(s.data))
	copy(data, s.data)
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

func (s *StreamFile) Release(ctx context.Context, fh fs.FileHandle) syscall.Errno {
	s.mu.Lock()
	if s.readers > 0 {
		s.readers--
	}
	s.mu.Unlock()
	return 0
}
