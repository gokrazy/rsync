package rsyncos

import (
	"io"

	"github.com/gokrazy/rsync/internal/log"
)

type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	DontRestrict bool

	logger log.Logger
}

func (s *Env) initLogger() {
	if s.logger == nil {
		s.logger = log.New(s.Stderr)
	}
}

func (s *Env) Logger() log.Logger {
	s.initLogger()
	return s.logger
}

func (s *Env) Logf(format string, v ...any) {
	s.initLogger()
	s.logger.Printf(format, v...)
}

func (s *Env) Restrict() bool { return !s.DontRestrict }
