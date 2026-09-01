package discord

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gustavosett/discord-unlocker/internal/proxy"
)

func TestChromiumProxyArguments(t *testing.T) {
	arguments, err := chromiumProxyArguments(proxy.Endpoint{Host: "8.8.8.8", Port: 1080})
	if err != nil {
		t.Fatalf("chromiumProxyArguments() error = %v", err)
	}

	want := []string{
		"--proxy-server=socks5://8.8.8.8:1080",
		"--proxy-bypass-list=cdn.discordapp.com;*.cdn.discordapp.com;cdn-b1.discordapp.com;cdn-b2.discordapp.com;media.discordapp.com;images-ext-*.discordapp.com;static.discord.com;static-edge.discord.com;*.discordapp.net;*.discord.media;*.storage.googleapis.com;<local>",
	}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("chromiumProxyArguments() = %#v, want %#v", arguments, want)
	}
}

func TestChromiumProxyArgumentsFormatsIPv6AsURIHost(t *testing.T) {
	arguments, err := chromiumProxyArguments(proxy.Endpoint{
		Host: "2606:4700:4700::1111",
		Port: 443,
	})
	if err != nil {
		t.Fatalf("chromiumProxyArguments() error = %v", err)
	}
	if got, want := arguments[0], "--proxy-server=socks5://[2606:4700:4700::1111]:443"; got != want {
		t.Fatalf("proxy argument = %q, want %q", got, want)
	}
}

func TestChromiumProxyArgumentsRejectsUnsafeEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint proxy.Endpoint
	}{
		{name: "private IPv4", endpoint: proxy.Endpoint{Host: "192.168.1.10", Port: 1080}},
		{name: "zero port", endpoint: proxy.Endpoint{Host: "8.8.8.8"}},
		{name: "hostname", endpoint: proxy.Endpoint{Host: "proxy.example", Port: 1080}},
		{name: "argument injection", endpoint: proxy.Endpoint{Host: "8.8.8.8 --no-sandbox", Port: 1080}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arguments, err := chromiumProxyArguments(test.endpoint)
			if !errors.Is(err, proxy.ErrInvalidEndpoint) {
				t.Fatalf("chromiumProxyArguments() error = %v, want proxy.ErrInvalidEndpoint", err)
			}
			if arguments != nil {
				t.Fatalf("chromiumProxyArguments() arguments = %#v, want nil", arguments)
			}
		})
	}
}
