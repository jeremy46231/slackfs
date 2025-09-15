package fs

import (
	"context"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	sl "github.com/jeremy46231/slackfs/internal/slack"
)

// ChannelDir represents a single Slack channel directory. It contains
// a text view file (<name>.txt) and a members.json file.
type ChannelDir struct {
	Directory
	client *sl.Client
	meta   ChannelMeta
}

func NewChannelDir(client *sl.Client, meta ChannelMeta) *ChannelDir {
	return &ChannelDir{client: client, meta: meta}
}

func (cd *ChannelDir) OnAdd(ctx context.Context) {
	// Inside the channel directory, create members.json (and later other files).
	members := NewMembersFile(cd)
	memInode := cd.NewPersistentInode(ctx, members, fs.StableAttr{Mode: uint32(fuse.S_IFREG)})
	cd.AttachChild("members.json", memInode, true)
}
