package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// ChannelTxtFile renders channel messages as plain text.
// For now, it holds an empty buffer and only supports writes that would
// eventually post to Slack.
type ChannelTxtFile struct {
	File
	ch *ChannelDir
}

func NewChannelTxtFile(ch *ChannelDir) *ChannelTxtFile {
	f := &ChannelTxtFile{File: *NewFile(), ch: ch}
	return f
}

func (f *ChannelTxtFile) OnAdd(ctx context.Context) {}

// Optionally override Write to forward to Slack via ch.client.
func (f *ChannelTxtFile) Write(ctx context.Context, fh fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	text := string(data)
	text = strings.TrimRight(text, "\r\n")
	fmt.Printf("ChannelTxt append: channel=%s id=%s text=%q\n", f.ch.meta.Name, f.ch.meta.ID, text)
	return uint32(len(data)), 0
}

// Channel text file is writable; allow read-only, write-only, or read-write opens.
func (f *ChannelTxtFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, fuse.FOPEN_DIRECT_IO, 0
}

// MembersFile returns JSON of members for the channel.
type MembersFile struct {
	File
	ch *ChannelDir
}

func NewMembersFile(ch *ChannelDir) *MembersFile {
	f := &MembersFile{File: *NewFile(), ch: ch}
	return f
}

func (f *MembersFile) OnAdd(ctx context.Context) {
	// Populate with placeholder JSON until Slack client has ListMembers.
	payload := []string{}
	b, _ := json.MarshalIndent(payload, "", "  ")
	f.mu.Lock()
	f.content = append([]byte{}, b...)
	f.mu.Unlock()
}

// Make members.json read-only from userspace.
func (f *MembersFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if flags&syscall.O_WRONLY != 0 || flags&syscall.O_RDWR != 0 {
		return nil, 0, syscall.EACCES
	}
	return nil, fuse.FOPEN_DIRECT_IO, 0
}
