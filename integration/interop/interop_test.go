package interop_test

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/internal/rsynctest"
	"github.com/gokrazy/rsync/internal/testlogger"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
)

func TestMain(m *testing.M) {
	if err := rsynctest.CommandMain(m); err != nil {
		log.Fatal(err)
	}
}

func TestRsyncVersion(t *testing.T) {
	// This function is not an actual test, just used to include the rsync
	// version in test output.
	rsync := exec.Command(rsynctest.AnyRsync(t), "--version")
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}
}

func TestTridgeRsyncVersion(t *testing.T) {
	// This function is not an actual test, just used to include the rsync
	// version in test output.
	rsyncBin := rsynctest.TridgeOrGTFO(t, "--version")
	tridgeRsync := exec.Command(rsyncBin, "--version")
	tridgeRsync.Stdout = testlogger.New(t)
	tridgeRsync.Stderr = testlogger.New(t)
	if err := tridgeRsync.Run(); err != nil {
		t.Fatalf("%v: %v", tridgeRsync.Args, err)
	}
}

func TestModuleListingServer(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: add reason")

	tmp := t.TempDir()

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(tmp))

	// request module list
	var buf bytes.Buffer
	rsync := exec.Command(rsyncBin,
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost")
	rsync.Stdout = &buf
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	output := buf.String()
	if want := "interop\tinterop"; !strings.Contains(output, want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, output)
	}
}

func TestModuleListingClient(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(tmp))

	// request module list
	args := []string{
		"gokr-rsync",
		"-aH",
		"rsync://localhost:" + srv.Port + "/",
	}
	stdout, _ := rsynctest.Output(t, args...)

	if want := "interop\tinterop"; !strings.Contains(string(stdout), want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, string(stdout))
	}
}

func TestModuleListingClientPort(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(tmp))

	// request module list
	args := []string{
		"gokr-rsync",
		"-aH",
		"--port=" + srv.Port,
		"rsync://localhost/",
	}
	stdout, _ := rsynctest.Output(t, args...)

	if want := "interop\tinterop"; !strings.Contains(string(stdout), want) {
		t.Fatalf("rsync output unexpectedly did not contain %q:\n%s", want, string(stdout))
	}
}

func TestModuleContentsListingDirs(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")

	// create files in source to be copied
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	dummy := filepath.Join(source, "dummy")
	want := []byte("heyo")
	if err := os.WriteFile(dummy, want, 0644); err != nil {
		t.Fatal(err)
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(tmp))

	// request module content listing
	rsync := exec.Command(rsynctest.AnyRsync(t),
		"--dirs",
		"--port="+srv.Port,
		"rsync://localhost/interop")
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}
}

func TestInterop(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	dest := filepath.Join(tmp, "dest")

	// create files in source to be copied
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	dummy := filepath.Join(source, "dummy")
	want := []byte("heyo")
	if err := os.WriteFile(dummy, want, 0644); err != nil {
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
	// 		if err := os.WriteFile(config, []byte(rsyncdConfig), 0644); err != nil {
	// 			t.Fatal(err)
	// 		}
	// 		srv := exec.Command(rsynctest.AnyRsync(t),
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
	rsync := exec.Command(rsynctest.AnyRsync(t),
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"--dry-run",
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	// sync into dest dir
	rsync = exec.Command(rsynctest.AnyRsync(t),
		//		"--debug=all4",
		"--archive",
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	{
		got, err := os.ReadFile(filepath.Join(dest, "dummy"))
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
	rsync = exec.Command(rsynctest.AnyRsync(t),
		//		"--debug=all4",
		"--archive",
		// TODO: should this be --checksum instead?
		"--ignore-times", // disable rsync’s “quick check”
		"-v", "-v", "-v", "-v",
		"--port="+srv.Port,
		"rsync://localhost/interop/", // copy contents of interop
		//source+"/", // sync from local directory
		dest) // directly into dest
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
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
		if err := os.WriteFile(dummy, []byte(subdir), 0644); err != nil {
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
		got, err := os.ReadFile(filepath.Join(dest, "dummy"))
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
		got, err := os.ReadFile(filepath.Join(dest, "cheap", "dummy"))
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
	t.Parallel()

	_, source, dest := createSourceFiles(t)

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	// sync into dest dir
	rsync := exec.Command(rsynctest.AnyRsync(t),
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"--port=" + srv.Port,
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}

func TestInteropSubdirExclude(t *testing.T) {
	t.Parallel()

	_, source, dest := createSourceFiles(t)

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	// sync into dest dir
	rsync := exec.Command(rsynctest.AnyRsync(t),
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				// TODO: implement support for include rules
				//"-f", "+ *.o",
				// NOTE: Using -f is the more modern replacement
				// for using --exclude like so:
				//"--exclude=dummy",
				"-f", "- expensive",
				"-v", "-v", "-v", "-v",
				"--port=" + srv.Port,
			}, "rsync://localhost/interop/"),
			dest)...)
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	expensiveFn := filepath.Join(dest, "expensive", "dummy")
	if _, err := os.ReadFile(expensiveFn); !os.IsNotExist(err) {
		t.Fatalf("ReadFile(%s) did not return -ENOENT, but %v", expensiveFn, err)
	}
	cheapFn := filepath.Join(dest, "cheap", "dummy")
	if _, err := os.ReadFile(cheapFn); err != nil {
		t.Fatalf("ReadFile(%s): %v", cheapFn, err)
	}
}

func TestInteropSubdirExcludeMultipleNested(t *testing.T) {
	t.Parallel()

	_, source, dest := createSourceFiles(t)

	nested := filepath.Join(source, "nested")

	// create files in source to be copied
	subDirs := []string{"nested-expensive", "nested-cheap"}
	for _, subdir := range subDirs {
		dummy := filepath.Join(nested, subdir, "dummy")
		if err := os.MkdirAll(filepath.Dir(dummy), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dummy, []byte(subdir), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// start a server to sync from
	srv := rsynctest.New(t, rsynctest.InteropModule(source))

	// sync into dest dir
	rsync := exec.Command(rsynctest.AnyRsync(t),
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				// TODO: implement support for include rules
				//"-f", "+ *.o",
				// NOTE: Using -f is the more modern replacement
				// for using --exclude like so:
				//"--exclude=dummy",
				"-f", "- nested/nested-expensive",
				"-f", "- nested/nested-cheap",
				"-v", "-v", "-v", "-v",
				"--port=" + srv.Port,
			}, "rsync://localhost/interop/"),
			dest)...)
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	expensiveFn := filepath.Join(dest, "nested", "nested-expensive", "dummy")
	if _, err := os.ReadFile(expensiveFn); !os.IsNotExist(err) {
		t.Fatalf("ReadFile(%s) did not return -ENOENT, but %v", expensiveFn, err)
	}
	cheapFn := filepath.Join(dest, "nested", "nested-cheap", "dummy")
	if _, err := os.ReadFile(cheapFn); !os.IsNotExist(err) {
		t.Fatalf("ReadFile(%s) did not return -ENOENT, but %v", cheapFn, err)
	}
}

func TestInteropRemoteCommand(t *testing.T) {
	t.Parallel()

	_, source, dest := createSourceFiles(t)

	sourcesArgs := []string{
		"localhost:" + source + "/expensive/", // copy contents of interop
	}
	if strings.HasPrefix(rsynctest.RsyncVersion(t), "3.") {
		sourcesArgs = append(sourcesArgs, ":"+source+"/cheap") // copy cheap directory
	}

	// sync into dest dir
	rsync := exec.Command(rsynctest.AnyRsync(t),
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"--protocol=27",
				"-v", "-v", "-v", "-v",
				"-e", os.Args[0],
			}, sourcesArgs...),
			dest)...)
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
	if err := rsync.Run(); err != nil {
		t.Fatalf("%v: %v", rsync.Args, err)
	}

	if err := sourceFullySyncedTo(t, dest); err != nil {
		t.Fatal(err)
	}
}

func TestInteropRemoteDaemon(t *testing.T) {
	t.Parallel()

	rsyncBin := rsynctest.TridgeOrGTFO(t, "https://github.com/gokrazy/rsync/issues/33")

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

	// sync into dest dir
	rsync := exec.Command(rsyncBin,
		append(
			append([]string{
				//		"--debug=all4",
				"--archive",
				"-v", "-v", "-v", "-v",
				"-e", os.Args[0],
			}, sourcesArgs(t)...),
			dest)...)
	rsync.Stdout = testlogger.New(t)
	rsync.Stderr = testlogger.New(t)
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

func TestInteropRemoteDaemonSSH(t *testing.T) {
	t.Parallel()

	// ensure the user running the tests (root when doing the privileged run!)
	// has an SSH private key:
	privKeyPath := filepath.Join(t.TempDir(), "ssh_private_key")
	genKey := exec.Command("ssh-keygen",
		"-N", "",
		"-t", "ed25519",
		"-f", privKeyPath)
	genKey.Stdout = testlogger.New(t)
	genKey.Stderr = testlogger.New(t)
	if err := genKey.Run(); err != nil {
		t.Fatalf("%v: %v", genKey.Args, err)
	}

	t.Run("Anon", func(t *testing.T) {
		t.Parallel()

		rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: reason")

		_, source, dest := createSourceFiles(t)

		// start a server to sync from
		srv := rsynctest.New(t,
			rsynctest.InteropModule(source),
			rsynctest.Listeners([]rsyncdconfig.Listener{
				{AnonSSH: "localhost:0"},
			}))

		// sync into dest dir
		rsync := exec.Command(rsyncBin,
			append(
				append([]string{
					//		"--debug=all4",
					"--archive",
					"-v", "-v", "-v", "-v",
					"-e", "ssh -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
				}, sourcesArgs(t)...),
				dest)...)
		// Ensure SSH_* environment variables (like SSH_ASKPASS or
		// SSH_AUTH_SOCK) do not leak into the test, otherwise tests
		// might be interrupted by a text UI password prompt.
		rsync.Env = []string{} // non-nil, but empty
		rsync.Stdout = testlogger.New(t)
		rsync.Stderr = testlogger.New(t)
		if err := rsync.Run(); err != nil {
			t.Fatalf("%v: %v", rsync.Args, err)
		}

		if err := sourceFullySyncedTo(t, dest); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("AuthorizedFail", func(t *testing.T) {
		t.Parallel()

		rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: reason")

		tmp, source, dest := createSourceFiles(t)

		authorizedKeysPath := filepath.Join(tmp, "authorized_keys")
		if err := os.WriteFile(authorizedKeysPath, []byte("# no keys authorized"), 0644); err != nil {
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
		rsync := exec.Command(rsyncBin,
			append(
				append([]string{
					//		"--debug=all4",
					"--archive",
					"-v", "-v", "-v", "-v",
					"-e", "ssh -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
				}, sourcesArgs(t)...),
				dest)...)
		rsync.Stdout = testlogger.New(t)
		rsync.Stderr = testlogger.New(t)
		if err := rsync.Run(); err == nil {
			t.Fatalf("rsync unexpectedly succeeded")
		}
	})

	t.Run("AuthorizedPass", func(t *testing.T) {
		t.Parallel()

		rsyncBin := rsynctest.TridgeOrGTFO(t, "TODO: reason")

		tmp, source, dest := createSourceFiles(t)

		authorizedKeysPath := filepath.Join(tmp, "authorized_keys")
		pubKey, err := os.ReadFile(privKeyPath + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(authorizedKeysPath, pubKey, 0644); err != nil {
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
		rsync := exec.Command(rsyncBin,
			append(
				append([]string{
					//		"--debug=all4",
					"--archive",
					"-v", "-v", "-v", "-v",
					"-e", "ssh -o IdentityFile=" + privKeyPath + " -o StrictHostKeyChecking=no -o CheckHostIP=no -o UserKnownHostsFile=/dev/null -p " + srv.Port,
				}, sourcesArgs(t)...),
				dest)...)
		rsync.Stdout = testlogger.New(t)
		rsync.Stderr = testlogger.New(t)
		if err := rsync.Run(); err != nil {
			t.Fatalf("%v: %v", rsync.Args, err)
		}

		if err := sourceFullySyncedTo(t, dest); err != nil {
			t.Fatal(err)
		}
	})
}
