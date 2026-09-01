package cli

import (
	"io"
	"testing"
)

func TestParseModes(t *testing.T) {
	tests := []struct {
		args []string
		want Mode
	}{
		{nil, ModeLaunch},
		{[]string{"--autostart"}, ModeAutostart},
		{[]string{"--check"}, ModeCheck},
		{[]string{"--version"}, ModeVersion},
	}
	for _, test := range tests {
		got, err := Parse(test.args, io.Discard)
		if err != nil {
			t.Fatalf("Parse(%v): %v", test.args, err)
		}
		if got.Mode != test.want {
			t.Fatalf("Parse(%v).Mode = %v, want %v", test.args, got.Mode, test.want)
		}
	}
}

func TestParseRejectsCombinedModesAndArguments(t *testing.T) {
	for _, args := range [][]string{{"--check", "--autostart"}, {"extra"}, {"--unknown"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatalf("Parse(%v) deveria falhar", args)
		}
	}
}
