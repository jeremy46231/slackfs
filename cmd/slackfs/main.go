package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	fusefs "github.com/hanwen/go-fuse/v2/fs"
	ifs "github.com/jeremy46231/slackfs/internal/fs"
)

func main() {
	mountPoint := flag.String("mountpoint", "", "path to mount slackfs")
	flag.Parse()
	if *mountPoint == "" {
		log.Fatalf("mountpoint must be provided")
	}
	// ensure directory exists
	if err := os.MkdirAll(*mountPoint, 0755); err != nil {
		log.Fatalf("cannot create mountpoint: %v", err)
	}

	// Create root and mount
	root := ifs.NewRoot()
	server, err := fusefs.Mount(*mountPoint, root, nil)
	if err != nil {
		log.Fatalf("mount failed: %v", err)
	}

	// Wait channel that closes when the server stops
	done := make(chan struct{})
	go func() {
		server.Wait()
		close(done)
	}()

	// Set up signal handler to unmount on SIGINT/SIGTERM.
	// On first signal, ask server to unmount cleanly. If it doesn't shut down
	// within timeout, try platform-specific forced unmount commands.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Printf("signal received, unmounting %s", *mountPoint)
		if err := server.Unmount(); err != nil {
			log.Printf("server.Unmount error: %v", err)
		}

		// wait up to 5s for server to stop
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
		}

		log.Printf("attempting forced unmount for %s", *mountPoint)
		// Try common unmount fallbacks per-platform. These may require sudo.
		switch runtime.GOOS {
		case "darwin":
			_ = exec.Command("umount", "-f", *mountPoint).Run()
			_ = exec.Command("diskutil", "unmount", "force", *mountPoint).Run()
		case "linux":
			_ = exec.Command("fusermount", "-uz", *mountPoint).Run()
			_ = exec.Command("umount", "-l", *mountPoint).Run()
		default:
			// no-op for other platforms
		}
	}()

	log.Printf("mounted %s", *mountPoint)
	<-done
	log.Printf("server stopped")
}
