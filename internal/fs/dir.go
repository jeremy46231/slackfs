package fs

import (
	"context"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// Directory is a reusable base for directories that hold child inodes.
// It embeds go-fuse's fs.Inode so the kernel sees it as a directory node.
type Directory struct {
	fs.Inode
	mu sync.RWMutex
}

// AttachChild safely attaches a child inode by name. The 'stable' flag keeps a stable mapping.
func (d *Directory) AttachChild(name string, inode *fs.Inode, stable bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.AddChild(name, inode, stable)
}

// Lookup finds existing children by name. We rely on AddChild/AttachChild population.
func (d *Directory) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	d.mu.RLock()
	child := d.GetChild(name)
	d.mu.RUnlock()
	if child == nil {
		return nil, syscall.ENOENT
	}
	// Provide sensible defaults so the kernel has correct perms before Getattr is invoked.
	if child.IsDir() {
		out.Mode = syscall.S_IFDIR | 0755
	} else {
		out.Mode = syscall.S_IFREG | 0644
	}
	// Ensure entries appear owned by the mounting user to avoid early EPERM on write opens.
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
	return child, 0
}

// Getattr reports basic directory attributes.
func (d *Directory) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = uint32(syscall.S_IFDIR | 0755)
	now := time.Now()
	out.SetTimes(nil, &now, &now)
	out.Owner = fuse.Owner{Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid())}
	return 0
}
