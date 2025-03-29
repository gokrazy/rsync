package rsyncos

import "io"

type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	DontRestrict bool
}

func (s *Env) Restrict() bool { return !s.DontRestrict }
