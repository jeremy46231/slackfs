warning, written by gpt5 with human review :3

---

# Plan for `slackfs`

## High-Level Goals

I want to create a novelty but fully usable Slack client that mounts Slack as a filesystem.  
The idea is that standard shell commands (`echo`, `tail -f`, etc.) become the way to interact with Slack.

**Core goals:**

- Treat Slack workspaces and channels as directories and files.
- Be able to send messages by appending to a file (`echo ... >> channel`).
- Be able to follow live channel activity using `tail -f channel`.
- Support Linux, macOS, and Windows.
- Provide a "native" experience via FUSE on each OS.
- Provide a packaged fallback VM (tiny Linux inside QEMU) for users who cannot or do not want to install FUSE drivers.
- Make it hackable and fun for other developers to try.

---

## Ideal Result

```bash
cd ~/slackfs

# Send a message to #lounge
echo "hello world" >> channels/#lounge.txt

# Send a message to a private channel
echo "status update: deployed" >> channels/#private-channel.txt

# Follow messages in #lounge live
tail -f channels/#lounge.txt

# Same but get raw JSON for each Slack event
tail -f channels/#lounge.jsonl
```

When I type these commands:

- The message goes into Slack in real time.
- Tailing shows other people’s messages as they arrive.
- Different extensions (.txt, .jsonl, etc.) allow for different views (human-readable vs structured).
  - idea: with no extension, it's a folder with files for other things, perhaps a directory of members, a .txt for the topic and description, etc
  - idea: .ansi version that includes colors and formatting codes all pretty

---

Desired OS Support

Primary:

- Linux: libfuse (native support).
- macOS: macFUSE (installable kernel extension).
- Windows: WinFsp (Windows FUSE-compatible layer).

Fallback / VM:

- Ship a tiny QEMU-based Linux VM with:
- BusyBox, Bash, slackfs binary, FUSE support.
- A simple launcher script per platform to start it and give users a shell.
- Purpose: if macFUSE/WinFsp is too hard to set up, users can still run slackfs inside a Linux VM shell.

---

Implementation Plan

Language & Libraries

- Go chosen for developer speed, good concurrency model, and cross-platform compilation.
- FUSE: github.com/hanwen/go-fuse/v2
- Slack API: slack-go/slack

Authentication

- User must provide a Slack token (xoxp-\*\*\*).
- For now: read from SLACK_TOKEN environment variable.
- Later: build helper flow (Node/Bun CLI or Go-based OAuth helper).

Filesystem Design

- Mount point shows Slack workspace as root directory.
- Each channel is represented as one or more files:
- #channel.txt → plain text messages.
- #channel.json → raw JSON message events.
- Writing to #channel.txt with O_APPEND translates to chat.postMessage.
- Tailing #channel.txt streams messages as they arrive from Slack’s Socket Mode.

Message Handling

- Outgoing: implement Write in FUSE handler. On echo >> file, parse input line(s), send to Slack API.
- Incoming: use Slack Socket Mode to subscribe to new messages. On new message, append to in-memory buffer and notify readers using go-fuse notifications (NotifyContent for data changes) and, when sizes change, attribute invalidation or short attr timeouts so tail -f readers see updates promptly.
- Maintain rate limiting and backoff according to Slack API rules.

Native Installation

- Linux: build native binary, require libfuse installed.
- macOS: build native binary, require macFUSE (user must install).
- Windows: build native binary, require WinFsp.

VM Fallback

- Build a minimal Linux image (Alpine-based).
- Include slackfs binary, BusyBox, Bash.
- Package with QEMU and launcher script:
- macOS: ./slackfs-vm.sh opens a terminal into the VM.
- Windows: .bat or PowerShell equivalent.
- Linux: optional, for users who don’t have libfuse.
- UX: user operates Slack from inside the VM shell using standard Linux tools.

⸻

Roadmap

1.  Skeleton FS

    - Mount a FUSE fs, expose one dummy file hello.txt.
    - Verify cat and tail -f work.

2.  Slack integration

    - Read SLACK_TOKEN from env.
    - Send message on echo >> file.
    - Display Slack messages in tail -f.

3.  Channel mapping

    - List channels as files under mount point.
    - Support .txt and .json.

4.  Cross-platform testing

    - Linux dev box.
    - macOS with macFUSE.
    - Windows with WinFsp.

5.  VM fallback

    - Package Alpine/QEMU image.
    - Provide launcher scripts for macOS/Windows/Linux.

6.  Polish

    - Provide CLI for login/token management.
    - Document installation steps for each platform.

---

Stretch Ideas

- Alternate output formats (.html, .md).
- Support files directly in a files/ directory.
- More slack features, like pins, channel topics/descriptions, member lists, etc.

---

Success Criteria

- I can run:

```bash
echo "it works" >> channels/#lounge.txt
tail -f channels/#lounge.txt
```

- Message appears in Slack #lounge, and I see new messages in real time with tail -f.
- Works on at least Linux + macOS.
- Windows users can either install WinFsp or fall back to the VM bundle.
