package rsyncos

import "io"

type Std struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}
