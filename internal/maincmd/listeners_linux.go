//go:build linux

package maincmd

import (
	"fmt"
	"net"

	"github.com/coreos/go-systemd/activation"
)

func systemdListeners() ([]net.Listener, error) {
	listeners, err := activation.Listeners()
	if err != nil {
		return nil, err
	}
	if len(listeners) == 0 {
		return nil, nil
	}
	if got, want := len(listeners), 1; got != want {
		return nil, fmt.Errorf("unexpected number of sockets received from systemd: got %d, want %d", got, want)
	}
	return listeners, nil
}
