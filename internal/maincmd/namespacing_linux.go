//go:build linux && !nonamespacing
// +build linux,!nonamespacing

package maincmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/rsyncd"
	"golang.org/x/sys/unix"
)

// based on https://medium.com/@teddyking/namespaces-in-go-mount-e4c04fe9fb29
func pivotRoot(newroot string) error {
	putold := filepath.Join(newroot, "/.pivot_root")

	// create putold directory
	if err := os.MkdirAll(putold, 0700); err != nil {
		return err
	}

	// bind mount newroot to itself - this is a slight hack
	// needed to work around a pivot_root requirement
	err := syscall.Mount(
		newroot,
		newroot,
		"",
		syscall.MS_BIND|syscall.MS_REC,
		"")
	if err != nil {
		return fmt.Errorf("mount(): %v", err)
	}

	// remount root as read-only: https://unix.stackexchange.com/questions/128336/why-doesnt-mount-respect-the-read-only-option-for-bind-mounts
	err = syscall.Mount(
		newroot,
		newroot,
		"",
		syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY,
		"")
	if err != nil {
		return fmt.Errorf("mount -o remount,ro: %v", err)
	}

	// TODO: when trying to use syscall.PivotRoot(".", ".") as described in
	// https://manpages.debian.org/bullseye/manpages-dev/pivot_root.2.en.html#NOTES,
	// I would end up with writeable rsync module mounts?!
	if err := syscall.PivotRoot(newroot, putold); err != nil {
		return fmt.Errorf("pivot_root(2): %v", err)
	}

	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir(/): %v", err)
	}

	// Only unmount the old root, but not delete it: we lack permission now that
	// the root is re-mounted read-only.
	putold = "/.pivot_root"
	if err := syscall.Unmount(putold, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount(%s): %v", putold, err)
	}

	return nil
}

func namespace(modules []rsyncd.Module, listen string) error {
	if os.Getenv("GOKRAZY_RSYNC_NAMESPACE") != "" {
		log.Printf("pid %d (inside Linux mount/pid namespace)", os.Getpid())

		// Expected by the go-systemd package, and hard to set before creating
		// the process in Go.
		os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))

		// Set mount point propagation to MS_SLAVE.
		// See https://hechao.li/2020/06/09/Mini-Container-Series-Part-1-Filesystem-Isolation/
		if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_SLAVE, ""); err != nil {
			return fmt.Errorf("mount(/, MS_SLAVE): %v", err)
		}

		// Create our own tmpfs mount so that we won’t end up with nodev,
		// nosuid, noexec and/or atime, to prevent operation not permitted
		// errors when remounting read-only():
		// https://unix.stackexchange.com/questions/655409/in-a-user-namespace-as-non-root-on-a-nosuid-nodev-filesystem-why-does-a-bind-m
		tmpdir, err := os.MkdirTemp("", "gokr-rsync")
		if err != nil {
			return err
		}
		if err := syscall.Mount("tmpfs", tmpdir, "tmpfs", syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("mount(tmpfs, %s): %v", tmpdir, err)
		}

		if err := os.Chdir(tmpdir); err != nil {
			return err
		}

		// prepare read-only bind mounts for each configured rsync module:
		log.Printf("mounting rsync modules read-only:")
		for _, mod := range modules {
			log.Printf("  rsync module %q from host=%s to namespace=/%s", mod.Name, mod.Path, mod.Name)
			// TODO: restrict module names to not contain slashes. does rsync do that?
			if err := os.MkdirAll(mod.Name, 0755); err != nil {
				return err
			}
			if err := syscall.Mount(mod.Path, mod.Name, "none", syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
				return err
			}
		}

		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		if err := pivotRoot(wd); err != nil {
			return fmt.Errorf("pivotRoot(%q): %v", wd, err)
		}

		if err := dropPrivileges(); err != nil {
			return fmt.Errorf("dropPrivileges: %v", err)
		}

		for idx, mod := range modules {
			mod.Path = "/" + mod.Name
			modules[idx] = mod
		}

		if err := canUnexpectedlyWriteTo("."); err != nil {
			return err
		}

		return nil
	}

	if os.Getuid() != 0 {
		version()
		log.Printf("environment: unprivileged")
		return nil
	}

	version()
	log.Printf("environment: privileged")
	log.Printf("creating Linux mount/pid namespace for read-only rsync module mounts")

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
		"GOKRAZY_RSYNC_NAMESPACE=1",
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWNS | unix.CLONE_NEWPID,
		GidMappingsEnableSetgroups: false,
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %v", cmd.Args, err)
	}
	return errIsParent
}

var errIsParent = errors.New("re-exec parent process sentinel error")
