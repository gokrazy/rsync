package sender

import "testing"

func TestPath(t *testing.T) {
	for _, tt := range []struct {
		requested string
		localDir  string
		wantRoot  string
		wantStrip string
	}{
		{
			requested: "/",              // sent by client
			localDir:  "/usr/share/man", // module.Path

			wantRoot:  "/usr/share/man",
			wantStrip: "/usr/share/man/",
		},

		{
			requested: "tr/man5",        // sent by client
			localDir:  "/usr/share/man", // module.Path

			wantRoot:  "/usr/share/man/tr/man5",
			wantStrip: "/usr/share/man/tr/",
		},

		{ // client started with src=/usr/share/man
			requested: "",               // sent by client
			localDir:  "/usr/share/man", // module.Path

			wantRoot:  "/usr/share/man",
			wantStrip: "/usr/share/",
		},

		{ // client started with src=/usr/share/man/
			requested: "/",              // sent by client
			localDir:  "/usr/share/man", // module.Path

			wantRoot:  "/usr/share/man",
			wantStrip: "/usr/share/man/",
		},
	} {
		t.Run("requested="+tt.requested, func(t *testing.T) {
			gotRoot, gotStrip := getRootStrip(tt.requested, tt.localDir)
			if gotRoot != tt.wantRoot {
				t.Errorf("unexpected root: got %q, want %q", gotRoot, tt.wantRoot)
			}
			if gotStrip != tt.wantStrip {
				t.Errorf("unexpected strip: got %q, want %q", gotStrip, tt.wantStrip)
			}
		})
	}
}
