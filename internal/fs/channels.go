package fs

import (
	"context"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	sl "github.com/jeremy46231/slackfs/internal/slack"
)

// ChannelsDir lists channels under the mount. It creates a child directory per channel
// and inside each provides files like #channel.txt and members.json.
type ChannelsDir struct {
	Directory
	client *sl.Client
}

func NewChannelsDir(client *sl.Client) *ChannelsDir {
	return &ChannelsDir{client: client}
}

func (cd *ChannelsDir) OnAdd(ctx context.Context) {
	// For now, create a placeholder channel pair: directory and txt file.
	meta := ChannelMeta{ID: "CEXAMPLE", Name: "example"}
	// channel directory (#name)
	chDir := NewChannelDir(cd.client, meta)
	chInode := cd.NewPersistentInode(ctx, chDir, fs.StableAttr{Mode: uint32(fuse.S_IFDIR)})
	cd.AttachChild(meta.Name, chInode, true)
	// channel text file sibling (#name.txt)
	chTxt := NewChannelTxtFile(chDir)
	txtInode := cd.NewPersistentInode(ctx, chTxt, fs.StableAttr{Mode: uint32(fuse.S_IFREG)})
	cd.AttachChild(meta.Name+".txt", txtInode, true)
}

// ChannelMeta is a minimal representation to avoid introducing Slack types in fs package.
type ChannelMeta struct {
	ID   string
	Name string
}
