//go:build !linux

package restrict

import "github.com/landlock-lsm/go-landlock/landlock"

// TODO: implement support for OpenBSD unveil(2)?

var ExtraHook func() []landlock.Rule

func MaybeFileSystem(_, _ []string) error { return nil }
