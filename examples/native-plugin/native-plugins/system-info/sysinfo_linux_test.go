package sysinfo

import "testing"

func TestParseProcStat(t *testing.T) {
	got, err := parseProcStat("cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 2 3 4 5 6 7 8 9 10\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.total != 1000 || got.busy != 150 {
		t.Errorf("parseProcStat = %+v, want busy 150 of 1000", got)
	}
	if _, err := parseProcStat("garbage"); err == nil {
		t.Error("garbage parsed")
	}
}

func TestMemoryAndLoad(t *testing.T) {
	m, err := Memory()
	if err != nil || m.Total == 0 || m.Available > m.Total {
		t.Fatalf("Memory = %+v, %v", m, err)
	}
	s, err := newSampler()
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.sample()
	if err != nil || v < 0 || v > 100 {
		t.Errorf("sample = %v, %v", v, err)
	}
}
