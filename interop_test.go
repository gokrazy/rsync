package rsync_test

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gokrazy/rsync/internal/maincmd"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "localhost" {
		// Strip first 2 args (./rsync.test localhost) from command line:
		// rsync(1) is calling this process as a remote shell.
		os.Args = os.Args[2:]
		if err := maincmd.Main(context.Background(), os.Args, os.Stdin, os.Stdout, os.Stderr, nil); err != nil {
			log.Fatal(err)
		}
	} else {
		os.Exit(m.Run())
	}
}

// TODO: non-empty exclusion list

func TestRsyncVersion(t *testing.T) {
	// This function is not an actual test, just used to include the rsync
	// version in test output.
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		"--version")
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}
}

func TestModuleListing(t *testing.T) {
	tmp := t.TempDir()

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(tmp))

	// request module list
	var buf bytes.Buffer
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost")
	rsync.Stdout = &buf
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	output := buf.String()
	if want := "interop\tinterop"; !strings.Contains(output, want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
	}
}

func TestInterop(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	// create files in source to be copied
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	dummy := filepath.Join(source, "dummy")
	want := []byte("heyo")
	if err := ioutil.WriteFile(dummy, want, 0644); err != nil {
		t.Fatal(err)
	}

	linkToDummy := filepath.Join(source, "link_to_dummy")
	if err := os.Symlink("dummy", linkToDummy); err != nil {
		t.Fatal(err)
	}

	if os.Getuid() == 0 {
		rsynctest.CreateDummyDeviceFiles(t, source)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	// 	{
	// 		config := filepath.Join(tmp, "rsyncd.conf")
	// 		rsyncdConfig := `
	// 	use chroot = no
	// 	# 0 = no limit
	// 	max connections = 0
	// 	pid file = ` + tmp + `/rsyncd.pid
	// 	exclude = lost+found/
	// 	transfer logging = yes
	// 	timeout = 900
	// 	ignore nonreadable = yes
	// 	dont compress   = *.gz *.tgz *.zip *.z *.Z *.rpm *.deb *.bz2 *.zst

	// 	[interop]
	// 	       path = /home/michael/i3/docs
	// #` + source + `
	// 	       comment = interop
	// 	       read only = yes
	// 	       list = true

	// 	`
	// 		if err := ioutil.WriteFile(config, []byte(rsyncdConfig), 0644); err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		srv := exec.Command("rsync",
	// 			"--daemon",
	// 			"--config="+config,
	// 			"--verbose",
	// 			"--address=localhost",
	// 			"--no-detach",
	// 			"--port=8730")
	// 		srv.Stdout = os.Stdout
	// 		srv.Stderr = os.Stderr
	// 		if err := srv.Start(); err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		go func() {
	// 			if err := srv.Wait(); err != nil {
	// 				t.Error(err)
	// 			}
	// 		}()
	// 		defer srv.Process.Kill()
	//
	//      time.Sleep(1 * time.Second)
	// 	}

	// dry run (slight differences in protocol)
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"--dry-run",
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	// sync into dest dir
	rsync = exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	{
		got, err := ioutil.ReadFile(filepath.Join(dest, "dummy"))
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}

	{
		got, err := os.Readlink(filepath.Join(dest, "link_to_dummy"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "dummy"; got != want {
			t.Fatalf("unexpected symlink target: got %q, want %q", got, want)
		}
	}

	if os.Getuid() == 0 {
		rsynctest.VerifyDummyDeviceFiles(t, source, dest)
	}

	// Run rsync again. This should not modify any files, but will result in
	// rsync sending sums to the sender.
	rsync = exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		//		"--debug=all4",
		"--archive",
		// TODO: should this be --checksum instead?
		"--ignore-times", // disable rsync’s “quick check”
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

}

func createSourceFiles(t *testing.T) (string, string, string) {
	t.Helper()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	// create files in source to be copied
	subDirs := []string{"expensive", "cheap"}
	for _, subdir := range subDirs {
		dummy := filepath.Join(source, subdir, "dummy")
		if err := os.MkdirAll(filepath.Dir(dummy), 0755); err != nil {
			t.Fatal(err)
		}
		if err := ioutil.WriteFile(dummy, []byte(subdir), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return tmp, source, dest
}

func sourcesArgs(t *testing.T) []string {
	if strings.HasPrefix(rsynctest.RsyncVersion(t), "3.") {
		// rsync 3.0.0 (March 2008) introduced multiple source args.
		return []string{
			"rsync://localhost/interop/expensive/", // copy contents of interop
			"rsync://localhost/interop/cheap",      // copy cheap directory
		}
	}
	// Older rsync only supports a single source arg.
	return []string{
		"rsync://localhost/interop/expensive/", // copy contents of interop
	}
}

func sourceFullySyncedTo(t *testing.T, dest string) error {
	{
		want := []byte("expensive")
		got, err := ioutil.ReadFile(filepath.Join(dest, "dummy"))
		if err != nil {
			return err
		}
		if diff := cmp.Diff(want, got); diff != "" {
			return fmt.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}

	if !strings.HasPrefix(rsynctest.RsyncVersion(t), "3.") {
		return nil
	}

	{
		want := []byte("cheap")
		got, err := ioutil.ReadFile(filepath.Join(dest, "cheap", "dummy"))
		if err != nil {
			return err
		}
		if diff := cmp.Diff(want, got); diff != "" {
			return fmt.Errorf("unexpected file contents: diff (-want +got):\n%s", diff)
		}
	}
	return nil
}

func TestInteropSubdir(t *testing.T) {
	_, source, dest := createSourceFiles(t)

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	// sync into dest dir
	rsync := exec.Command("rsync", //"/home/michael/src/openrsync/openrsync",
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"--port=" + srv.Port,
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}

func TestInteropRemoteCommand(t *testing.T) {
	_, source, dest := createSourceFiles(t)

	sourcesArgs := []string{
		"localhost:" + source + "/expensive/", // copy contents of interop
	}
	if strings.HasPrefix(rsynctest.RsyncVersion(t), "3.") {
		sourcesArgs = append(sourcesArgs, ":"+source+"/cheap") // copy cheap directory
	}

	// sync into dest dir
	rsync := exec.Command("rsync", //*/ "/home/michael/src/openrsync/openrsync",
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"--protocol=27",
				"-v", "-v", "-v", "-v",
				"-e", os.Args[0],
			}, sourcesArgs...),
			dest)...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}

func TestInteropRemoteDaemon(t *testing.T) {
	tmp, source, dest := createSourceFiles(t)

	homeDir := filepath.Join(tmp, "home")
	// Use os.Setenv so that the os.UserConfigDir() call below returns the
	// correct path.
	os.Setenv("HOME", homeDir)
	os.Setenv("XDG_CONFIG_HOME", homeDir+"/.config")

	{
		// in remote daemon mode, rsync needs a config file, so we create one and
		// set the HOME environment variable such that gokr-rsyncd will pick it up.
		cfg := rsyncdconfig.Config{
			Modules: []rsyncd.Module{
				{Name: "interop", Path: source},
			},
		}
		configDir, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(configDir, "gokr-rsyncd.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(configPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := toml.NewEncoder(f).Encode(&cfg); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// TODO: this does not seem to work when using openrsync?
	// does openrsync send the wrong command?

	// sync into dest dir
	rsync := exec.Command("rsync", //*/ "/home/michael/src/openrsync/openrsync",
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"-e", os.Args[0],
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	// TODO: does os.Environ() reflect changes by os.Setenv()?
	rsync.Env = append(os.Environ(),
		"HOME="+homeDir,
		"XDG_CONFIG_HOME="+homeDir+"/.config")
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}

func TestInteropRemoteDaemonAnonSSH(t *testing.T) {
	tmp, source, dest := createSourceFiles(t)

	// start a server to sync from
	srv := rsynctest.New(t,
		rsynctest.InteropModule(source),
		rsynctest.Listeners([]rsyncdconfig.Listener{
			{AnonSSH: "localhost:0"},
		}))

	// ensure the user running the tests (root when doing the privileged run!)
	// has an SSH private key:
	privKeyPath := filepath.Join(tmp, "ssh_private_key")
	genKey := exec.Command("ssh-keygen",
		"-N", "",
		"-t", "ed25519",
		"-f", privKeyPath)
	genKey.Stdout = os.Stdout
	genKey.Stderr = os.Stderr
	if err := genKey.Run(); err != nil {
		t.Fatalf("%v: %v", genKey.Args, err)
	}

	// sync into dest dir
	rsync := exec.Command("rsync", //*/ "/home/michael/src/openrsync/openrsync",
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"-e", "ssh -vv -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}

func TestInteropRemoteDaemonAuthorizedSSHFail(t *testing.T) {
	tmp, source, dest := createSourceFiles(t)

	// ensure the user running the tests (root when doing the privileged run!)
	// has an SSH private key:
	privKeyPath := filepath.Join(tmp, "ssh_private_key")
	genKey := exec.Command("ssh-keygen",
		"-N", "",
		"-t", "ed25519",
		"-f", privKeyPath)
	genKey.Stdout = os.Stdout
	genKey.Stderr = os.Stderr
	if err := genKey.Run(); err != nil {
		t.Fatalf("%v: %v", genKey.Args, err)
	}

	authorizedKeysPath := filepath.Join(tmp, "authorized_keys")
	if err := ioutil.WriteFile(authorizedKeysPath, []byte("# no keys authorized"), 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t,
		rsynctest.InteropModule(source),
		rsynctest.Listeners([]rsyncdconfig.Listener{
			{
				AuthorizedSSH: rsyncdconfig.SSHListener{
					Address:        "localhost:0",
					AuthorizedKeys: authorizedKeysPath,
				},
			},
		}))

	// sync into dest dir
	rsync := exec.Command("rsync", //*/ "/home/michael/src/openrsync/openrsync",
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"-e", "ssh -vv -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err == nil {
		t.Fatalf("rsync unexpectedly succeeded")
	}
}

func TestInteropRemoteDaemonAuthorizedSSHPass(t *testing.T) {
	tmp, source, dest := createSourceFiles(t)

	// ensure the user running the tests (root when doing the privileged run!)
	// has an SSH private key:
	privKeyPath := filepath.Join(tmp, "ssh_private_key")
	genKey := exec.Command("ssh-keygen",
		"-N", "",
		"-t", "ed25519",
		"-f", privKeyPath)
	genKey.Stdout = os.Stdout
	genKey.Stderr = os.Stderr
	if err := genKey.Run(); err != nil {
		t.Fatalf("%v: %v", genKey.Args, err)
	}

	authorizedKeysPath := filepath.Join(tmp, "authorized_keys")
	pubKey, err := ioutil.ReadFile(privKeyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(authorizedKeysPath, pubKey, 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t,
		rsynctest.InteropModule(source),
		rsynctest.Listeners([]rsyncdconfig.Listener{
			{
				AuthorizedSSH: rsyncdconfig.SSHListener{
					Address:        "localhost:0",
					AuthorizedKeys: authorizedKeysPath,
				},
			},
		}))

	// sync into dest dir
	rsync := exec.Command("rsync", //*/ "/home/michael/src/openrsync/openrsync",
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"-e", "ssh -vv -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}
