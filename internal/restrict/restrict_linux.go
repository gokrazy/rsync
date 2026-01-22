// Package restrict can be used to restrict further file system access of the
// process if the operating system provides an API for that.
package restrict

import (
	"fmt"
	"log"
	"os"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// ExtraHook is set when testing to make the landlock rule set more permissive.
var ExtraHook func() []landlock.Rule

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
			// rsync needs /etc/passwd and /etc/group for user/group lookup.
			//
			// As of Go 1.24, the net package Go resolver reads
			// the following DNS configurations files:
			//
			// - /etc/resolv.conf
			// - /etc/hosts
			// - /etc/services
			// - /etc/nsswitch.conf
			//
			// Because the /etc/resolv.conf file might be re-created (by DHCP
			// clients, Tailscale, or similar), we need to provide the entire
			// /etc directory instead of individual files. Otherwise, the
			// program seems to work at first and then fails DNS resolution
			// after a while.
			landlock.RODirs("/etc").IgnoreIfMissing(),
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
