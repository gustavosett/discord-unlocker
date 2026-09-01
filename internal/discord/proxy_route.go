package discord

import (
	"fmt"

	"github.com/gustavosett/discord-unlocker/internal/proxy"
)

// discordProxyBypassList keeps Discord's media, CDN and Google-backed storage
// traffic off the SOCKS5 route. Chromium also interprets <local> as hostnames
// without a dot.
const discordProxyBypassList = "cdn.discordapp.com;*.cdn.discordapp.com;cdn-b1.discordapp.com;cdn-b2.discordapp.com;media.discordapp.com;images-ext-*.discordapp.com;static.discord.com;static-edge.discord.com;*.discordapp.net;*.discord.media;*.storage.googleapis.com;<local>"

func chromiumProxyArguments(endpoint proxy.Endpoint) ([]string, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, fmt.Errorf("validate Discord proxy endpoint: %w", err)
	}

	return []string{
		"--proxy-server=socks5://" + endpoint.Address(),
		"--proxy-bypass-list=" + discordProxyBypassList,
	}, nil
}
