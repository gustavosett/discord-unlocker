package winutil

import "testing"

func TestIsAutostart(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "flag", args: []string{"discord-unlocker.exe", "--autostart"}, want: true},
		{name: "explicit true", args: []string{"--autostart=true"}, want: true},
		{name: "numeric true", args: []string{"--autostart=1"}, want: true},
		{name: "single dash", args: []string{"-autostart"}, want: true},
		{name: "explicit false", args: []string{"--autostart=false"}, want: false},
		{name: "manual", args: []string{"discord-unlocker.exe"}, want: false},
		{name: "similar flag", args: []string{"--autostarted"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsAutostart(test.args); got != test.want {
				t.Fatalf("IsAutostart(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
