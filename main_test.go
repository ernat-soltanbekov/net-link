package main

import (
	"io"
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"2525"}, {"-l", "-p", "2525", "-s", "127.0.0.1"}, {"-c", "-N", "-v", "-w", "3", "localhost", "2525"}, {"--tui", "::1", "8989"}, {"-c", "-z", "localhost", "8989"}, {"--help"}} {
		if _, ok := parse(args); !ok {
			t.Errorf("rejected %q", args)
		}
	}
	for _, args := range [][]string{{"2525", "localhost"}, {"0"}, {"65536"}, {"-1"}, {"abc"}, {"999999999999999999999999999999999"}, {"-p"}, {"-p", "2", "3"}, {"-c"}, {"-l", "-c", "localhost", "3"}, {"--tui", "-N", "host", "3"}, {"--tui", "-c", "host", "3"}, {"-w", "2"}, {"-c", "-w", "301", "host", "3"}, {"-z"}, {"-c", "-p", "2", "host", "3"}, {"-v", "-v"}, {"--log", ""}, {"--help", "bad"}} {
		if _, ok := parse(args); ok {
			t.Errorf("accepted %q", args)
		}
	}
	o, ok := parse(nil)
	if !ok || o.port != "8989" {
		t.Fatal(o)
	}
}
func TestExactUsageAndHelp(t *testing.T) {
	var out, diagnostic strings.Builder
	if run([]string{"2525", "localhost"}, io.NopCloser(strings.NewReader("")), &out, &diagnostic) != 2 || out.String() != usage || diagnostic.Len() != 0 {
		t.Fatal(out.String(), diagnostic.String())
	}
	out.Reset()
	if run([]string{"--help"}, io.NopCloser(strings.NewReader("")), &out, &diagnostic) != 0 || !strings.Contains(out.String(), "--tui") {
		t.Fatal("help")
	}
}
func FuzzArguments(f *testing.F) {
	for _, s := range []string{"2525", "65536", "0", "-1", "99999999999999999999999999", "-c", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		o, ok := parse([]string{s})
		if ok {
			n, valid := number(o.port, 65535)
			if !valid || n < 1 || n > 65535 {
				t.Fatal(o)
			}
		}
	})
}
