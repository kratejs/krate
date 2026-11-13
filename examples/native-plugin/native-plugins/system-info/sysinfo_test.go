package sysinfo

import (
	"runtime"
	"testing"
)

func TestInfo(t *testing.T) {
	i, err := Info()
	if err != nil {
		t.Fatal(err)
	}
	if i.OS != runtime.GOOS || i.CPUs < 1 || i.Hostname == "" {
		t.Errorf("Info = %+v", i)
	}
}

func TestLoadBetween(t *testing.T) {
	cases := []struct {
		a, b cpuTimes
		want float64
	}{
		{cpuTimes{10, 100}, cpuTimes{60, 200}, 50},
		{cpuTimes{10, 100}, cpuTimes{10, 200}, 0},
		{cpuTimes{10, 100}, cpuTimes{10, 100}, 0},
		{cpuTimes{10, 100}, cpuTimes{500, 200}, 100},
	}
	for _, c := range cases {
		if got := loadBetween(c.a, c.b); got != c.want {
			t.Errorf("loadBetween(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
