package rsyncdconfig_test

import (
	"testing"

	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/rsyncd"
	"github.com/google/go-cmp/cmp"
)

func TestConfig(t *testing.T) {
	cfg, err := rsyncdconfig.FromString(`
[[listener]]
rsyncd = "localhost:873"

[[listener]]
http_monitoring = "localhost:8738"

[[listener]]
anon_ssh = "localhost:22873"

[[module]]
name = "interop"
path = "/non/existant/path"

`)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(cfg.Listeners), 3; got != want {
		t.Fatalf("unexpected number of listeners: got %d, want %d", got, want)
	}

	{
		want := []rsyncdconfig.Listener{
			{Rsyncd: "localhost:873"},
			{HTTPMonitoring: "localhost:8738"},
			{AnonSSH: "localhost:22873"},
		}
		if diff := cmp.Diff(want, cfg.Listeners); diff != "" {
			t.Fatalf("unexpected listener config: diff (-want +got):\n%s", diff)
		}
	}

	{
		want := []rsyncd.Module{
			{Name: "interop", Path: "/non/existant/path"},
		}
		if diff := cmp.Diff(want, cfg.Modules); diff != "" {
			t.Fatalf("unexpected module config: diff (-want +got):\n%s", diff)
		}
	}
}
