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
	"github.com/hanwen/go-fuse/v2/fuse"
	ifs "github.com/jeremy46231/slackfs/internal/fs"
)

func forceUnmount(mountPoint string) {
	// Try common unmount fallbacks per-platform. These may require sudo.
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("umount", "-f", mountPoint).Run()
		_ = exec.Command("diskutil", "unmount", "force", mountPoint).Run()
	case "linux":
		_ = exec.Command("fusermount", "-uz", mountPoint).Run()
		_ = exec.Command("umount", "-l", mountPoint).Run()
	default:
		// no-op for other platforms
	}
}

func main() {
	mountPoint := flag.String("mountpoint", "", "path to mount slackfs")
	flag.Parse()
	if *mountPoint == "" {
		log.Fatalf("mountpoint must be provided")
	}

	// Validate environment - check for SLACK_TOKEN if we had Slack integration
	// For now, just verify basic environment sanity
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		log.Printf("Warning: slackfs may not work correctly on %s", runtime.GOOS)
	}

	// ensure directory exists
	if err := os.MkdirAll(*mountPoint, 0755); err != nil {
		log.Fatalf("cannot create mountpoint: %v", err)
	}

	// Create root and mount with proper options
	root := ifs.NewRoot()
	opts := &fusefs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther: false, // Only allow mounting user to access
			Debug:      false, // Set to true for debugging
		},
	}
	server, err := fusefs.Mount(*mountPoint, root, opts)
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
	// Handle subsequent signals more aggressively.
	sigs := make(chan os.Signal, 2) // buffer for multiple signals
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		signalCount := 0
		for sig := range sigs {
			signalCount++
			log.Printf("signal %v received (%d), unmounting %s", sig, signalCount, *mountPoint)

			if signalCount == 1 {
				// First signal: try graceful unmount
				if err := server.Unmount(); err != nil {
					log.Printf("server.Unmount error: %v", err)
				}
			} else {
				// Subsequent signals: force unmount immediately
				log.Printf("forcing immediate unmount due to repeated signals")
				forceUnmount(*mountPoint)
				os.Exit(1)
			}

			// wait up to 5s for server to stop
			select {
			case <-done:
				return
			case <-time.After(5 * time.Second):
			}

			log.Printf("attempting forced unmount for %s", *mountPoint)
			forceUnmount(*mountPoint)
		}
	}()

	log.Printf("mounted %s", *mountPoint)
	<-done
	log.Printf("server stopped")
}
