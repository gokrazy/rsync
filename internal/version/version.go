package version

import (
	"runtime/debug"
)

func Read() string {
	info, ok := debug.ReadBuildInfo()
	mainVersion := info.Main.Version
	if !ok {
		mainVersion = "<runtime/debug.ReadBuildInfo failed>"
	}
	return "gokrazy/rsync " + mainVersion
}
