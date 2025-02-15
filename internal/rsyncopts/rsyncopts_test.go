package rsyncopts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// See testdata/_tridge_rsync_dump_table.patch for the corresponding
// C code (dump_long_options()) we use to compare the parsing result.
func dumpTable(buf *strings.Builder, pc *Context) {
	for _, opt := range pc.table {
		longName := opt.longName
		if longName == "" {
			longName = "(null)"
		}
		fmt.Fprintf(buf, "long=%s short=%s arg=", longName, opt.shortName)
		switch opt.argInfo {
		case POPT_ARG_STRING:
			if opt.arg == nil {
				fmt.Fprintf(buf, "\"(null)\"\n")
			} else {
				stringPtr := opt.arg.(*string)
				if stringPtr == nil {
					fmt.Fprintf(buf, "\"(null)\"\n")
				} else {
					if *stringPtr == "" {
						fmt.Fprintf(buf, "\"(null)\"\n")
					} else {
						fmt.Fprintf(buf, "%q\n", *stringPtr)
					}
				}
			}
		case POPT_ARG_NONE,
			POPT_ARG_INT,
			POPT_ARG_VAL,
			POPT_BIT_SET:
			if opt.arg == nil {
				fmt.Fprintf(buf, "<nil int>\n")
			} else {
				intPtr := opt.arg.(*int)
				if intPtr == nil {
					fmt.Fprintf(buf, "<nil int>\n")
				} else {
					fmt.Fprintf(buf, "%d\n", *intPtr)
				}
			}
		default:
			fmt.Fprintf(buf, "unknown argInfo: %v", opt.argInfo)
		}
	}
}

func discardKnownDifferences(lines []string) []string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		// TODO: document why ipv4/ipv6 have different values
		ignore := strings.HasPrefix(line, "long=ipv4 ") ||
			strings.HasPrefix(line, "long=ipv6 ") ||
			// We implement protocol version 27 currently,
			// tridge rsync implements newer versions.
			strings.HasPrefix(line, "long=protocol ")
		if !ignore {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func TestParseArguments(t *testing.T) {
	for _, tt := range []struct {
		args        []string
		goldenTable string
	}{
		{
			// --recursive (-r) is ARG_VAL with val==2
			// --delete is ARG_NONE
			args:        []string{"-rtO", "--delete"},
			goldenTable: "rsync-rtO--delete.txt",
		},
		{
			args:        []string{"--backup-dir=/tmp/backups"},
			goldenTable: "rsync-backup-dir.txt",
		},
		{
			// --temp-dir (-T) is ARG_STRING
			args:        []string{"-T", "/tmp"},
			goldenTable: "rsync-T-tmp.txt",
		},
		{
			args:        []string{"-T=/tmp"},
			goldenTable: "rsync-T_tmp.txt",
		},
		{
			args:        []string{"-T/tmp"},
			goldenTable: "rsync-Ttmp.txt",
		},
		{
			// --checksum-seed is ARG_INT
			args:        []string{"--checksum-seed", "2342"},
			goldenTable: "rsync-checksum-seed-2342.txt",
		},
		{
			// --delete-missing-args is BIT_SET
			args:        []string{"--delete-missing-args"},
			goldenTable: "rsync-delete-missing-args.txt",
		},
		{
			args:        []string{"--ignore-missing-args"},
			goldenTable: "rsync-ignore-missing-args.txt",
		},
		{
			args:        []string{"--ignore-missing-args", "--delete-missing-args"},
			goldenTable: "rsync-ignore-delete-missing-args.txt",
		},
		{
			args:        []string{"--no-motd"},
			goldenTable: "rsync-no-motd.txt",
		},
		{
			// --verbose (-v) has val='v'
			args:        []string{"-vvv"},
			goldenTable: "rsync-vvv.txt",
		},
		{
			args:        []string{"-a"},
			goldenTable: "rsync-a.txt",
		},
		{
			args:        []string{"-P"},
			goldenTable: "rsync-P.txt",
		},
		{
			args:        []string{"--debug=del2,acl"},
			goldenTable: "rsync-debug-del2-acl.txt",
		},
		{
			args:        []string{"--info=name2"},
			goldenTable: "rsync-info-name2.txt",
		},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", tt.goldenTable))
			if err != nil {
				t.Fatal(err)
			}
			wantLines := discardKnownDifferences(strings.Split(strings.TrimSpace(string(want)), "\n"))

			pc, err := ParseArguments(tt.args, false)
			if err != nil {
				t.Fatalf("ParseArguments: %v", err)
			}

			var buf strings.Builder
			dumpTable(&buf, pc)
			gotLines := discardKnownDifferences(strings.Split(strings.TrimSpace(buf.String()), "\n"))

			if diff := cmp.Diff(wantLines, gotLines); diff != "" {
				t.Errorf("unexpected option state: diff (-tridge +gokrazy):\n%s", diff)
			}
		})
	}
}

func TestParseArgumentsError(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want int32
	}{
		{
			args: []string{"--delete=thoroughly"},
			want: POPT_ERROR_UNWANTEDARG,
		},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := ParseArguments(tt.args, false)
			if err == nil {
				t.Fatalf("ParseArguments unexpectedly did not fail!")
			}
			got := err.(*PoptError).Errno
			if got != tt.want {
				t.Errorf("unexpected error: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseArgumentsRemaining(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want []string
	}{
		{
			args: []string{"-aH", "-e", "./rsync.test", "localhost:/tmp/src/", "/tmp/dst"},
			want: []string{"localhost:/tmp/src/", "/tmp/dst"},
		},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			pc, err := ParseArguments(tt.args, false)
			if err != nil {
				t.Fatalf("ParseArguments: %v", err)
			}
			got := pc.RemainingArgs
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("RemainingArgs: unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
