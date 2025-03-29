package rsynctest

import (
	"os"

	"github.com/gokrazy/rsync/internal/restrict"
	"github.com/landlock-lsm/go-landlock/landlock"
)

func init() {
	restrict.ExtraHook = func() []landlock.Rule {
		return []landlock.Rule{
			// contains /usr/bin/rsync (and library deps)
			landlock.RODirs("/usr"),

			// for t.TempDir()
			landlock.RWDirs(os.TempDir()).WithRefer(),

			// used in some of our test code
			landlock.RWFiles("/dev/null"),
		}
	}
}
