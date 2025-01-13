//go:build !linux

package maincmd

import "net"

func systemdListeners() ([]net.Listener, error) {
	return nil, nil
}
