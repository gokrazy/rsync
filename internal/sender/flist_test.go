package sender

import "testing"

func TestPath(t *testing.T) {
	for _, tt := range []struct {
		requested  string
		localDir   string
		trimPrefix string
		wantRoot   string
		wantStrip  string
	}{
		{
			requested:  "man/",           // sent by client
			localDir:   "/usr/share/man", // module.Path
			trimPrefix: "man/",           // module.Name + "/"

			wantRoot:  "/usr/share/man",
			wantStrip: "/usr/share/man/",
		},

		{
			requested:  "man/tr/man5",    // sent by client
			localDir:   "/usr/share/man", // module.Path
			trimPrefix: "man/",           // module.Name + "/"

			wantRoot:  "/usr/share/man/tr/man5",
			wantStrip: "/usr/share/man/tr/",
		},

		{ // client started with src=/usr/share/man
			requested:  "man",            // sent by client
			localDir:   "/usr/share/man", // module.Path
			trimPrefix: "man",            // module.Name + "/"

			wantRoot:  "/usr/share/man",
			wantStrip: "/usr/share/",
		},

		{ // client started with src=/usr/share/man/
			requested:  "man/",           // sent by client
			localDir:   "/usr/share/man", // module.Path
			trimPrefix: "man/",           // module.Name + "/"

			wantRoot:  "/usr/share/man",
			wantStrip: "/usr/share/man/",
		},
	} {
		t.Run("requested="+tt.requested, func(t *testing.T) {
			gotRoot, gotStrip := getRootStrip(tt.requested, tt.localDir, tt.trimPrefix)
			if gotRoot != tt.wantRoot {
				t.Errorf("unexpected root: got %q, want %q", gotRoot, tt.wantRoot)
			}
			if gotStrip != tt.wantStrip {
				t.Errorf("unexpected strip: got %q, want %q", gotStrip, tt.wantStrip)
			}
		})
	}
}
