package sender

import "testing"

func TestPath(t *testing.T) {
	for _, tt := range []struct {
		requested string
		wantRoot  string
		wantStrip string
	}{
		{
			requested: "/", // sent by client
			wantStrip: "",
		},

		{
			requested: "tr/man5", // sent by client
			wantStrip: "",
		},

		{
			requested: "tr/", // sent by client
			wantStrip: "tr/",
		},

		{ // client started with src=/usr/share/man
			requested: "", // sent by client
			wantStrip: "",
		},
	} {
		t.Run("requested="+tt.requested, func(t *testing.T) {
			gotStrip := getStrip(tt.requested)
			if gotStrip != tt.wantStrip {
				t.Errorf("unexpected strip: got %q, want %q", gotStrip, tt.wantStrip)
			}
		})
	}
}
