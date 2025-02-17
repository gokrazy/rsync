//go:build linux && !nonamespacing

package maincmd

import (
	"fmt"
	"syscall"

	"github.com/gokrazy/rsync/internal/log"
)

func dropPrivileges() error {
	if syscall.Getuid() != 0 {
		return nil
	}

	log.Printf("running as root (uid 0), dropping privileges to nobody (uid/gid 65534)")
	if err := syscall.Setgid(65534); err != nil {
		return fmt.Errorf("setgid(65534): %v", err)
	}

	if err := syscall.Setuid(65534); err != nil {
		return fmt.Errorf("setuid(65534): %v", err)
	}

	// Defense in depth: exit if we can re-gain uid/gid 0 permission:
	if err := syscall.Setgid(0); err == nil {
		return fmt.Errorf("unexpectedly able to re-gain gid 0 permission!")
	}

	if err := syscall.Setuid(0); err == nil {
		return fmt.Errorf("unexpectedly able to re-gain uid 0 permission!")
	}

	return nil
}
