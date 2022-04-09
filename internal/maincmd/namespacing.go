//go:build !linux || nonamespacing
// +build !linux nonamespacing

package maincmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/rsyncd"
)

func namespace(modules []rsyncd.Module, listen string) error {
	if os.Getenv("GOKRAZY_RSYNC_PRIVDROP") != "" {
		log.Printf("pid %d (privileges dropped)", os.Getpid())

		// Expected by the go-systemd package, and hard to set before creating
		// the process in Go.
		os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))

		return nil
	}

	if os.Getuid() != 0 {
		version()
		log.Printf("environment: unprivileged")
		return nil
	}

	version()
	log.Printf("environment: privileged")
	log.Printf("running as root (uid 0), dropping privileges to nobody (uid/gid 65534)")

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Create the listener while still running as uid 0 and inherit it, so that
	// we can listen on port 873 (rsync), which requires CAP_NET_BIND_SERVICE.
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = "/"
	// TODO: clean the environment
	cmd.Env = append(os.Environ(),
		"GOKRAZY_RSYNC_PRIVDROP=1",
		"LISTEN_FDS=1", // ExtraFiles start at 3
		"PATH=/bin:"+os.Getenv("PATH"))
	cmd.Stdin = os.Stdin // for interactive debugging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	lnFile, err := ln.(*net.TCPListener).File()
	if err != nil {
		return err
	}
	cmd.ExtraFiles = []*os.File{lnFile}
	runAsUnprivilegedUser(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %v", cmd.Args, err)
	}
	return errIsParent
}

var errIsParent = errors.New("re-exec parent process sentinel error")
