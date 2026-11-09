package main

import "testing"

func TestParseCleanFlags(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"clean", "--cache", "--dist", "--all", "proj"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.CleanCache || !flags.CleanDist || !flags.CleanAll {
		t.Errorf("clean flags not parsed: %+v", flags)
	}
	if len(rest) != 2 || rest[0] != "clean" || rest[1] != "proj" {
		t.Errorf("remaining args = %v", rest)
	}
}
