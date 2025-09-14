# slackfs - mount Slack as a filesystem

This project provides a FUSE-based filesystem that exposes Slack channels and messages as directories and files, letting you interact with Slack using standard shell tools.

```bash
go mod tidy && go mod vendor
go build ./cmd/slackfs
./slackfs -mountpoint /tmp/slackfs-mnt

# ctrl-c will try to unmount cleanly, if a shell is still open to the mountpoint it will force quit in 5 seconds
pgrep -fl slackfs | awk '/slackfs -mountpoint/{print $1}' | xargs -I{} kill -INT {} || true
# ensure it's cleaned up (macos specific)
umount /tmp/slackfs-mnt || true && rm -rf /tmp/slackfs-mnt
```
