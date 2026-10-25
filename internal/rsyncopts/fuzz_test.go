package rsyncopts_test

import (
	"io"
	"strings"
	"testing"

	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsynctest"
)

func FuzzParseArguments(f *testing.F) {
	f.Fuzz(func(t *testing.T, argv string) {
		osenv := &rsyncos.Env{
			Stdout: rsynctest.NopClose(io.Discard),
			Stderr: rsynctest.NopClose(io.Discard),
		}
		pc := rsyncopts.NewContext(rsyncopts.NewOptionsWithGokrazyDefaults(osenv))
		// If ParseArguments panics or exits, that is a fuzzer finding.
		pc.ParseArguments(osenv, strings.Split(argv, "\n"))
	})
}
