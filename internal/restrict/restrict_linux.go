// Package restrict can be used to restrict further file system access of the
// process if the operating system provides an API for that.
package restrict

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// ExtraHook is set when testing to make the landlock rule set more permissive.
var ExtraHook func() []landlock.Rule

// As of Go 1.24, the net package Go resolver reads
// the following DNS configurations files:
var dnsLookup = []string{
	"/etc/resolv.conf",
	"/etc/hosts",
	"/etc/services",
	"/etc/nsswitch.conf",
}

var userLookup = []string{
	"/etc/passwd", // user lookup
	"/etc/group",  // group lookup
}

// ssh(1) needs to read its config and key files
var sshConfigDirs = []string{
	filepath.Join(os.Getenv("HOME"), ".ssh"), // user
	"/etc/ssh",                               // system-wide
}
var sshDirs = []string{
	"/usr", // for running ssh(1)
	"/nix", // for running ssh(1) on NixOS
}
var sshDevices = []string{
	"/dev/null",
}

func MaybeFileSystem(roDirsOrFiles []string, rwDirs []string) error {
	re := ExtraHook
	if re == nil {
		re = func() []landlock.Rule {
			return nil
		}
	}
	var roDirs, roFiles []string
	for _, fn := range roDirsOrFiles {
		st, err := os.Stat(fn)
		if err != nil {
			return err
		}
		if st.IsDir() {
			roDirs = append(roDirs, fn)
		} else {
			roFiles = append(roFiles, fn)
		}
	}
	log.Printf("setting up landlock ACL (paths ro: %q, paths rw: %q)", roDirs, rwDirs)
	err := landlock.V3.BestEffort().RestrictPaths(
		append(re(), []landlock.Rule{
			landlock.ROFiles(dnsLookup...).IgnoreIfMissing(),
			landlock.ROFiles(userLookup...).IgnoreIfMissing(),
			landlock.RODirs(sshConfigDirs...).IgnoreIfMissing(),
			landlock.RODirs(sshDirs...).IgnoreIfMissing(),
			landlock.RWFiles(sshDevices...).IgnoreIfMissing(),
			landlock.RODirs(roDirs...).IgnoreIfMissing(),
			landlock.ROFiles(roFiles...).IgnoreIfMissing(),
			landlock.RWDirs(rwDirs...).WithRefer(),
		}...)...)
	if err != nil {
		return fmt.Errorf("landlock: %v", err)
	}

	// Check whether landlock worked and print the result to the log.
	//
	// We use /sys because that path should never be required
	// for regular functioning, yet is standard enough to be present
	// on all supported Linux versions (including gokrazy).
	const verifyPath = "/sys"
	_, err = os.ReadDir(verifyPath)
	if err == nil {
		log.Printf("landlock seems ineffective: readdir(%s) unexpectedly worked!", verifyPath)
	} else {
		log.Printf("landlock verified: readdir(%s) = %v", verifyPath, err)
	}

	return nil
}
