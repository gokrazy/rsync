package receivermaincmd

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/log"
)

// rsync/clientserver.c:start_socket_client
func socketClient(osenv osenv, opts *Opts, src, dest string) (*Stats, error) {
	u, err := url.Parse(src)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host += ":873" // rsync daemon port
	}
	log.Printf("Opening TCP connection to %s", host)
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return nil, err
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return nil, fmt.Errorf("empty remote path")
	}
	module := path
	if idx := strings.IndexByte(module, '/'); idx > -1 {
		module = module[:idx]
	}
	log.Printf("rsync module %q, path %q", module, path)
	if err := startInbandExchange(opts, conn, module, path); err != nil {
		return nil, err
	}
	stats, err := clientRun(osenv, opts, conn, dest, false)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// rsync/clientserver.c:start_inband_exchange
func startInbandExchange(opts *Opts, conn io.ReadWriter, module, path string) error {
	rd := bufio.NewReader(conn)

	// send client greeting
	fmt.Fprintf(conn, "@RSYNCD: %d\n", rsync.ProtocolVersion)

	// read server greeting
	serverGreeting, err := rd.ReadString('\n')
	if err != nil {
		return fmt.Errorf("ReadString: %v", err)
	}
	serverGreeting = strings.TrimSpace(serverGreeting)
	const serverGreetingPrefix = "@RSYNCD: "
	if !strings.HasPrefix(serverGreeting, serverGreetingPrefix) {
		return fmt.Errorf("invalid server greeting: got %q", serverGreeting)
	}
	// protocol negotiation: require at least version 27
	serverGreeting = strings.TrimPrefix(serverGreeting, serverGreetingPrefix)
	var remoteProtocol, remoteSub int32
	if _, err := fmt.Sscanf(serverGreeting, "%d.%d", &remoteProtocol, &remoteSub); err != nil {
		if _, err := fmt.Sscanf(serverGreeting, "%d", &remoteProtocol); err != nil {
			return fmt.Errorf("reading server greeting: %v", err)
		}
	}
	if remoteProtocol < 27 {
		return fmt.Errorf("server version %d too old", remoteProtocol)
	}

	log.Printf("(Client) Protocol versions: remote=%d, negotiated=%d", remoteProtocol, rsync.ProtocolVersion)
	log.Printf("Client checksum: md4")

	// send module name
	fmt.Fprintf(conn, "%s\n", module)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return fmt.Errorf("did not get server startup line: %v", err)
		}
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "@RSYNCD: AUTHREQD ") {
			// TODO: implement support for authentication
			return fmt.Errorf("authentication not yet implemented")
		}

		if line == "@RSYNCD: OK" {
			break
		}

		// TODO: @RSYNCD: EXIT after listing modules

		if strings.HasPrefix(line, "@ERROR") {
			fmt.Fprintf(os.Stderr, "%s\n", line)
			return fmt.Errorf("abort (rsync fatal error)")
		}

		// print rsync server message of the day (MOTD)
		fmt.Fprintf(os.Stdout, "%s\n", line)
	}

	sargv := serverOptions(opts)
	sargv = append(sargv, ".")
	if path != "" {
		sargv = append(sargv, path)
	}
	log.Printf("sending daemon args: %s", sargv)
	for _, argv := range sargv {
		fmt.Fprintf(conn, "%s\n", argv)
	}
	fmt.Fprintf(conn, "\n")

	return nil
}
