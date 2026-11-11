package memstat

import (
	"slices"
	"strings"
	"testing"
)

const rollupFixture = `55d3a1c00000-7ffd5a3f5000 ---p 00000000 00:00 0                          [rollup]
Rss:               22480 kB
Pss:               11200 kB
Pss_Dirty:          5100 kB
Pss_Anon:           4800 kB
Shared_Clean:      14000 kB
Shared_Dirty:        100 kB
Private_Clean:      3000 kB
Private_Dirty:      5380 kB
Referenced:        22480 kB
Anonymous:          5400 kB
LazyFree:              0 kB
Swap:                 64 kB
SwapPss:              32 kB
Locked:                0 kB
`

func TestParseSmapsRollup(t *testing.T) {
	p, err := parseSmaps(strings.NewReader(rollupFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := Proc{RSS: 22480 << 10, PSS: 11200 << 10, PrivateDirty: 5380 << 10, PrivateClean: 3000 << 10, Anon: 5400 << 10, Swap: 64 << 10}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}
}

func TestParseSmapsSumsMappings(t *testing.T) {
	const smaps = `00400000-00452000 r-xp 00000000 08:02 173521      /usr/bin/dbus-daemon
Size:                328 kB
Rss:                 100 kB
Pss:                  40 kB
Shared_Clean:         80 kB
Private_Clean:        10 kB
Private_Dirty:        10 kB
Anonymous:            10 kB
Swap:                  0 kB
VmFlags: rd ex mr mw me dw sd
00e03000-00e24000 rw-p 00000000 00:00 0                                  [heap]
Size:                132 kB
Rss:                  50 kB
Pss:                  50 kB
Private_Dirty:        50 kB
Anonymous:            50 kB
Swap:                  8 kB
`
	p, err := parseSmaps(strings.NewReader(smaps))
	if err != nil {
		t.Fatal(err)
	}
	if p.RSS != 150<<10 || p.PSS != 90<<10 || p.PrivateDirty != 60<<10 || p.PrivateClean != 10<<10 || p.Anon != 60<<10 || p.Swap != 8<<10 {
		t.Fatalf("got %+v", p)
	}
}

func TestParseStatPPID(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"123 (bash) S 99 123 123 0 -1", 99, true},
		{"123 (we ird) name)) R 7 1", 7, true},
		{"garbage", 0, false},
		{"1 (x)", 0, false},
	} {
		got, ok := parseStatPPID(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseStatPPID(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDescendantsOf(t *testing.T) {
	parent := map[int]int{2: 1, 3: 1, 4: 2, 5: 4, 9: 8, 1: 0}
	got := descendantsOf(1, parent)
	if !slices.Equal(got, []int{2, 3, 4, 5}) {
		t.Fatalf("got %v", got)
	}
	// A parent cycle must not loop.
	if got := descendantsOf(1, map[int]int{2: 1, 1: 2}); !slices.Equal(got, []int{2}) {
		t.Fatalf("cycle: got %v", got)
	}
}

func TestWebKitRole(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1/WebKitWebProcess": RoleWebviewWeb,
		"WebKitNetworkProcess": RoleWebviewNetwork,
		"WebKitGPUProcess":     RoleWebviewGPU,
		"WebKitWebProcess.exe": RoleWebviewWeb,
		"WebKitOtherThing":     RoleWebviewOther,
		"bwrap":                "",
		"krate-shell":          "",
	} {
		if got := WebKitRole(in); got != want {
			t.Errorf("WebKitRole(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReportByRole(t *testing.T) {
	r := &Report{Procs: []Proc{
		{PID: 1, Role: RoleWebviewWeb, PSS: 10, PrivateDirty: 4},
		{PID: 2, Role: RoleShell, PSS: 5, PrivateDirty: 2},
		{PID: 3, Role: RoleWebviewWeb, PSS: 20, PrivateDirty: 6},
	}}
	got := r.ByRole()
	if len(got) != 2 || got[0].Role != RoleShell || got[1].Role != RoleWebviewWeb {
		t.Fatalf("roles: %+v", got)
	}
	if got[1].PSS != 30 || got[1].PrivateDirty != 10 {
		t.Fatalf("web: %+v", got[1])
	}
}
