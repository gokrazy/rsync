package anonssh

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/google/shlex"
	"golang.org/x/crypto/ssh"
)

type mainFunc func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error

type anonssh struct {
	cfg  *rsyncdconfig.Config
	main mainFunc
}

// env is a Environment Variable request as per RFC4254 6.4.
type env struct {
	VariableName  string
	VariableValue string
}

// execR is a Command request as per RFC4254 6.5.
type execR struct {
	Command string
}

type session struct {
	env     []string
	ptyf    *os.File
	ttyf    *os.File
	channel ssh.Channel
	anonssh *anonssh
}

func (s *session) request(ctx context.Context, req *ssh.Request) error {
	_ = ctx // FIXME: not yet used

	switch req.Type {

	case "env":
		var r env
		if err := ssh.Unmarshal(req.Payload, &r); err != nil {
			return err
		}

		log.Printf("env request: %s=%s", r.VariableName, r.VariableValue)
		//s.env = append(s.env, fmt.Sprintf("%s=%s", r.VariableName, r.VariableValue))

	case "exec":
		var r execR
		if err := ssh.Unmarshal(req.Payload, &r); err != nil {
			return err
		}

		cmdline, err := shlex.Split(r.Command)
		if err != nil {
			return err
		}

		log.Printf("cmdline: %q", cmdline)
		// 2021/09/12 21:25:34 cmdline: ["rsync" "--server" "--daemon" "."]
		go func() {
			stderr := s.channel.Stderr()
			err := s.anonssh.main(cmdline, s.channel, s.channel, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "%s\n", err)
			}

			status := make([]byte, 4)
			if err != nil {
				binary.BigEndian.PutUint32(status, 1)
			}

			// See https://tools.ietf.org/html/rfc4254#section-6.10
			if _, err := s.channel.SendRequest("exit-status", false /* wantReply */, status); err != nil {
				log.Printf("err2: %v", err)
			}
			s.channel.Close()
		}()

		// stdout, err := cmd.StdoutPipe()
		// if err != nil {
		// 	return err
		// }
		// stdin, err := cmd.StdinPipe()
		// if err != nil {
		// 	return err
		// }
		// stderr, err := cmd.StderrPipe()
		// if err != nil {
		// 	return err
		// }
		// cmd.SysProcAttr.Setsid = true

		// if err := cmd.Start(); err != nil {
		// 	return err
		// }

		// req.Reply(true, nil)

		// go io.Copy(s.channel, stdout)
		// go io.Copy(s.channel.Stderr(), stderr)
		// go func() {
		// 	io.Copy(stdin, s.channel)
		// 	stdin.Close()
		// }()

		// go func() {
		// 	if err := cmd.Wait(); err != nil {
		// 		log.Printf("err: %v", err)
		// 	}
		// 	status := make([]byte, 4)
		// 	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		// 		binary.BigEndian.PutUint32(status, uint32(ws.ExitStatus()))
		// 	}

		// 	// See https://tools.ietf.org/html/rfc4254#section-6.10
		// 	if _, err := s.channel.SendRequest("exit-status", false /* wantReply */, status); err != nil {
		// 		log.Printf("err2: %v", err)
		// 	}
		// 	s.channel.Close()
		// }()
		return nil

	default:
		return fmt.Errorf("unknown request type: %q", req.Type)
	}

	return nil
}

func (as *anonssh) handleSession(newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept channel (%s)", err)
		return
	}

	// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
	go func(channel ssh.Channel, requests <-chan *ssh.Request) {
		ctx, canc := context.WithCancel(context.Background())
		defer canc()
		s := session{channel: channel, anonssh: as}
		for req := range requests {
			if err := s.request(ctx, req); err != nil {
				log.Printf("request(%q): %v", req.Type, err)
				errmsg := []byte(err.Error())
				// Append a trailing newline; the error message is
				// displayed as-is by ssh(1).
				if errmsg[len(errmsg)-1] != '\n' {
					errmsg = append(errmsg, '\n')
				}
				req.Reply(false, errmsg)
				channel.Write(errmsg)
				channel.Close()
			}
		}
		log.Printf("SSH requests exhausted")
	}(channel, requests)
}

func (as *anonssh) handleChannel(newChan ssh.NewChannel) {
	switch t := newChan.ChannelType(); t {
	case "session":
		as.handleSession(newChan)
	default:
		newChan.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %q", t))
		return
	}
}

func genHostKey(keyPath string) ([]byte, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	x509b, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	privateKeyPEM := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509b,
	}
	f, err := os.OpenFile(keyPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b := pem.EncodeToMemory(privateKeyPEM)
	if _, err := f.Write(b); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	return b, nil
}

type Listener struct {
	hostKey            ssh.Signer
	authorizedKeys     map[string]bool
	authorizedKeysPath string
}

func ListenerFromConfig(cfg rsyncdconfig.Listener) (*Listener, error) {
	hostKey, err := loadHostKey()
	if err != nil {
		return nil, err
	}

	var authorizedKeys map[string]bool
	if cfg.AuthorizedSSH.Address != "" {
		if cfg.AuthorizedSSH.AuthorizedKeys == "" {
			return nil, fmt.Errorf("authorized_keys not specified")
		}

		var err error
		authorizedKeys, err = loadAuthorizedKeys(cfg.AuthorizedSSH.AuthorizedKeys)
		if err != nil {
			return nil, err
		}
	}

	return &Listener{
		hostKey:            hostKey,
		authorizedKeys:     authorizedKeys,
		authorizedKeysPath: cfg.AuthorizedSSH.AuthorizedKeys,
	}, nil
}

func loadHostKey() (ssh.Signer, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "gokr-rsyncd", "ssh_host_ed25519_key")
	b, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, err
			}
			b, err = genHostKey(path)
			if err != nil {
				return nil, err
			}
			// fall-through
		} else {
			return nil, err
		}
	}
	return ssh.ParsePrivateKey(b)
}

func loadAuthorizedKeys(path string) (map[string]bool, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result := make(map[string]bool)

	s := bufio.NewScanner(bytes.NewReader(b))
	for lineNum := 1; s.Scan(); lineNum++ {
		if tr := strings.TrimSpace(s.Text()); tr == "" || strings.HasPrefix(tr, "#") {
			continue
		}
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey(s.Bytes())

		// This warning can be removed once the mentioned issue is resolved
		if keyType := pubKey.Type(); keyType == "ssh-rsa" {
			log.Printf("Warning: ignoring unsupported ssh-rsa key in %s:%d (see https://github.com/gokrazy/breakglass/issues/11)", path, lineNum)
		}

		if err != nil {
			return nil, err
		}
		result[string(pubKey.Marshal())] = true
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func Serve(ln net.Listener, listener *Listener, cfg *rsyncdconfig.Config, main mainFunc) error {
	as := &anonssh{
		main: main,
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			if listener.authorizedKeys == nil {
				log.Printf("user %q successfully authorized from remote addr %s", conn.User(), conn.RemoteAddr())
				return nil, nil
			}
			if listener.authorizedKeys[string(pubKey.Marshal())] {
				log.Printf("user %q successfully authorized from remote addr %s", conn.User(), conn.RemoteAddr())
				return nil, nil
			}
			return nil, fmt.Errorf("public key not found in %s", listener.authorizedKeysPath)
		},
	}

	config.AddHostKey(listener.hostKey)

	log.Printf("SSH host key fingerprint: %s", ssh.FingerprintSHA256(listener.hostKey.PublicKey()))

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			log.Printf("accept: %v", err)
			continue
		}

		go func(conn net.Conn) {
			_, chans, reqs, err := ssh.NewServerConn(conn, config)
			if err != nil {
				log.Printf("handshake: %v", err)
				return
			}

			// discard all out of band requests
			go ssh.DiscardRequests(reqs)

			for newChannel := range chans {
				as.handleChannel(newChannel)
			}
		}(conn)
	}
}
