package maincmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncstats"
)

// rsync/clientserver.c:start_socket_client
func socketClient(ctx context.Context, osenv *rsyncos.Env, opts *rsyncopts.Options, host string, path string, port int, paths []string) (*rsyncstats.TransferStats, error) {
	if port < 0 {
		if port := opts.RsyncPort(); port > 0 {
			host += ":" + strconv.Itoa(port)
		} else {
			host += ":873" // rsync daemon port
		}
	} else {
		host += ":" + strconv.Itoa(port)
	}
	dialer := net.Dialer{
		// Prefer the Go resolver: We know which files it uses (which makes life
		// easier for the restrict package), whereas the C resolver can be
		// extended by host-specific plugins.
		Resolver: &net.Resolver{
			PreferGo: true,
		},
	}
	timeoutStr := ""
	if timeout := opts.ConnectTimeoutSeconds(); timeout > 0 {
		dialer.Timeout = time.Duration(timeout) * time.Second
		timeoutStr = fmt.Sprintf(" (timeout: %d seconds)", timeout)
	}
	osenv.Logf("Opening TCP connection to %s%s", host, timeoutStr)
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	module := path
	if idx := strings.IndexByte(module, '/'); idx > -1 {
		module = module[:idx]
	}
	osenv.Logf("rsync module %q, path %q", module, path)
	done, err := startInbandExchange(osenv, opts, conn, module, path)
	if err != nil {
		return nil, err
	}
	if done {
		return nil, nil
	}
	stats, err := ClientRun(osenv, opts, conn, paths, false)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// rsync/clientserver.c:start_inband_exchange
func startInbandExchange(osenv *rsyncos.Env, opts *rsyncopts.Options, conn io.ReadWriter, module, path string) (done bool, _ error) {
	rd := bufio.NewReader(conn)

	// send client greeting
	fmt.Fprintf(conn, "@RSYNCD: %d\n", rsync.ProtocolVersion)

	// read server greeting
	serverGreeting, err := rd.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("ReadString: %v", err)
	}
	serverGreeting = strings.TrimSpace(serverGreeting)
	const serverGreetingPrefix = "@RSYNCD: "
	if !strings.HasPrefix(serverGreeting, serverGreetingPrefix) {
		return false, fmt.Errorf("invalid server greeting: got %q", serverGreeting)
	}
	// protocol negotiation: require at least version 27
	serverGreeting = strings.TrimPrefix(serverGreeting, serverGreetingPrefix)
	var remoteProtocol, remoteSub int32
	if _, err := fmt.Sscanf(serverGreeting, "%d.%d", &remoteProtocol, &remoteSub); err != nil {
		if _, err := fmt.Sscanf(serverGreeting, "%d", &remoteProtocol); err != nil {
			return false, fmt.Errorf("reading server greeting: %v", err)
		}
	}
	if remoteProtocol < 27 {
		return false, fmt.Errorf("server version %d too old", remoteProtocol)
	}

	if opts.Verbose() {
		osenv.Logf("(Client) Protocol versions: remote=%d, negotiated=%d", remoteProtocol, rsync.ProtocolVersion)
		osenv.Logf("Client checksum: md4")
	}

	// send module name
	fmt.Fprintf(conn, "%s\n", module)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("did not get server startup line: %v", err)
		}
		line = strings.TrimSpace(line)
		if opts.DebugGTE(rsyncopts.DEBUG_PROTO, 1) {
			osenv.Logf("read line: %q", line)
		}

		if strings.HasPrefix(line, "@RSYNCD: AUTHREQD ") {
			// TODO: implement support for authentication
			return false, fmt.Errorf("authentication not yet implemented")
		}

		if line == "@RSYNCD: OK" {
			break
		}

		if line == "@RSYNCD: EXIT" {
			return true, nil
		}

		if strings.HasPrefix(line, "@ERROR") {
			fmt.Fprintf(osenv.Stderr, "%s\n", line)
			return false, fmt.Errorf("abort (rsync fatal error)")
		}

		if opts.OutputMOTD() {
			// print rsync server message of the day (MOTD)
			fmt.Fprintf(osenv.Stdout, "%s\n", line)
		}
	}

	sargv := opts.ServerOptions()
	sargv = append(sargv, ".")
	if path != "" {
		sargv = append(sargv, path)
	}
	if opts.Verbose() {
		osenv.Logf("sending daemon args: %s", sargv)
	}
	for _, argv := range sargv {
		fmt.Fprintf(conn, "%s\n", argv)
	}
	fmt.Fprintf(conn, "\n")

	return false, nil
}
