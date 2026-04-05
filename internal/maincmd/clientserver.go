package maincmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/gokrazy/rsync"
	"github.com/gokrazy/rsync/internal/restrict"
	"github.com/gokrazy/rsync/internal/rsyncopts"
	"github.com/gokrazy/rsync/internal/rsyncos"
	"github.com/gokrazy/rsync/internal/rsyncstats"
	"github.com/mmcloughlin/md4"
)

// rsync/clientserver.c:start_socket_client
func socketClient(ctx context.Context, osenv *rsyncos.Env, opts *rsyncopts.Options, host string, remotePath string, port int, paths []string, roDirs, rwDirs []string) (*rsyncstats.TransferStats, error) {
	// Extract user[:password]@ from host (daemon protocol only).
	// Password may be percent-encoded from net/url parsing.
	var urlUser, urlPass string
	if idx := strings.IndexByte(host, '@'); idx > -1 {
		userinfo := host[:idx]
		host = host[idx+1:]
		if ci := strings.IndexByte(userinfo, ':'); ci > -1 {
			urlUser = userinfo[:ci]
			urlPass, _ = url.PathUnescape(userinfo[ci+1:])
		} else {
			urlUser = userinfo
		}
	}

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
	if osenv.Restrict() {
		if err := restrict.MaybeFileSystem(roDirs, rwDirs); err != nil {
			return nil, err
		}
	}
	done, err := startInbandExchange(osenv, opts, conn, remotePath, urlUser, urlPass)
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

// StartInbandExchange is the public API for daemon-over-remote-shell
// and the rsyncclient package. Auth credentials come from env/file only.
func StartInbandExchange(osenv *rsyncos.Env, opts *rsyncopts.Options, conn io.ReadWriter, remotePath string) (done bool, _ error) {
	return startInbandExchange(osenv, opts, conn, remotePath, "", "")
}

// rsync/clientserver.c:start_inband_exchange
func startInbandExchange(osenv *rsyncos.Env, opts *rsyncopts.Options, conn io.ReadWriter, remotePath string, urlUser, urlPass string) (done bool, _ error) {
	module := remotePath
	if idx := strings.IndexByte(module, '/'); idx > -1 {
		module = module[:idx]
	}
	osenv.Logf("rsync module %q, path %q", module, remotePath)

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
			challenge := strings.TrimPrefix(line, "@RSYNCD: AUTHREQD ")
			authUser := resolveUsername(urlUser)
			pass, err := getPassword(opts, urlPass)
			if err != nil {
				return false, fmt.Errorf("authentication required: %v", err)
			}
			hash := generateAuthHash(pass, challenge)
			fmt.Fprintf(conn, "%s %s\n", authUser, hash)
			continue
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
	sargv = append(sargv, remotePath)
	if opts.Verbose() {
		osenv.Logf("sending daemon args: %s", sargv)
	}
	for _, argv := range sargv {
		fmt.Fprintf(conn, "%s\n", argv)
	}
	fmt.Fprintf(conn, "\n")

	return false, nil
}

// resolveUsername returns the auth username from (in priority order):
// URL user, RSYNC_USERNAME env, current OS user, or "nobody".
func resolveUsername(urlUser string) string {
	if urlUser != "" {
		return urlUser
	}
	if u := os.Getenv("RSYNC_USERNAME"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "nobody"
}

// getPassword returns the auth password from (in priority order):
// URL password, --password-file, or RSYNC_PASSWORD env.
func getPassword(opts *rsyncopts.Options, urlPass string) (string, error) {
	if urlPass != "" {
		return urlPass, nil
	}
	if f := opts.PasswordFile(); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", fmt.Errorf("reading password file: %v", err)
		}
		lines := strings.SplitN(string(data), "\n", 2)
		return strings.TrimRight(lines[0], "\r"), nil
	}
	if p := os.Getenv("RSYNC_PASSWORD"); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("no password supplied (set RSYNC_PASSWORD or use --password-file)")
}

// generateAuthHash computes the rsync CSUM_MD4_OLD auth response:
// MD4(seed + password + challenge), base64-encoded without padding.
// CSUM_MD4_OLD prepends a 4-byte little-endian seed (0 for auth)
// before the password and challenge data.
func generateAuthHash(password, challenge string) string {
	h := md4.New()
	h.Write([]byte{0, 0, 0, 0})
	h.Write([]byte(password))
	h.Write([]byte(challenge))
	digest := h.Sum(nil)
	return base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString(digest)
}
