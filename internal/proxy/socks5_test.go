package proxy

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestDialSOCKS5ValidatesHandshakeAndAllReplyAddressTypes(t *testing.T) {
	t.Parallel()
	replies := map[string][]byte{
		"IPv4":   {0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0x01, 0xbb},
		"domain": {0x05, 0x00, 0x00, 0x03, 3, 'f', 'o', 'o', 0x01, 0xbb},
		"IPv6": append(
			[]byte{0x05, 0x00, 0x00, 0x04},
			append(make([]byte, net.IPv6len), 0x01, 0xbb)...,
		),
	}
	for name, reply := range replies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requestedHost string
			var requestedPort uint16
			dialer := scriptedSOCKS5Dialer(t, []byte{0x05, 0x00}, reply, func(host string, port uint16) {
				requestedHost, requestedPort = host, port
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			connection, err := dialSOCKS5(ctx, dialer, Endpoint{Host: "8.8.8.8", Port: 1080}, "gateway.discord.gg:443")
			if err != nil {
				t.Fatalf("dialSOCKS5() error = %v", err)
			}
			_ = connection.Close()
			if requestedHost != "gateway.discord.gg" || requestedPort != 443 {
				t.Fatalf("SOCKS5 target = %s:%d", requestedHost, requestedPort)
			}
		})
	}
}

func TestDialSOCKS5RejectsMalformedOrFailedReplies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		methodReply  []byte
		connectReply []byte
	}{
		{name: "wrong method version", methodReply: []byte{0x04, 0x00}},
		{name: "authentication required", methodReply: []byte{0x05, 0x02}},
		{name: "connect denied", methodReply: []byte{0x05, 0x00}, connectReply: []byte{0x05, 0x05, 0x00, 0x01}},
		{name: "bad reserved byte", methodReply: []byte{0x05, 0x00}, connectReply: []byte{0x05, 0x00, 0x01, 0x01}},
		{name: "unknown address type", methodReply: []byte{0x05, 0x00}, connectReply: []byte{0x05, 0x00, 0x00, 0x09}},
		{name: "empty bound hostname", methodReply: []byte{0x05, 0x00}, connectReply: []byte{0x05, 0x00, 0x00, 0x03, 0x00}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialer := scriptedSOCKS5Dialer(t, test.methodReply, test.connectReply, nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			connection, err := dialSOCKS5(ctx, dialer, Endpoint{Host: "1.1.1.1", Port: 1080}, "gateway.discord.gg:443")
			if connection != nil {
				_ = connection.Close()
			}
			if err == nil {
				t.Fatal("dialSOCKS5() succeeded with malformed reply")
			}
		})
	}
}

func TestDialSOCKS5HonorsContextDuringHandshake(t *testing.T) {
	t.Parallel()
	dialer := func(_ context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return client, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := dialSOCKS5(ctx, dialer, Endpoint{Host: "9.9.9.9", Port: 1080}, "gateway.discord.gg:443")
	if err == nil {
		t.Fatal("dialSOCKS5() ignored context cancellation")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("dialSOCKS5() took %s after context deadline", elapsed)
	}
}

func scriptedSOCKS5Dialer(
	t *testing.T,
	methodReply []byte,
	connectReply []byte,
	inspect func(host string, port uint16),
) DialContextFunc {
	t.Helper()
	return func(_ context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			greeting := make([]byte, 3)
			if _, err := io.ReadFull(server, greeting); err != nil {
				return
			}
			if err := writeAll(server, methodReply); err != nil || len(methodReply) != 2 || methodReply[0] != 0x05 || methodReply[1] != 0x00 {
				return
			}
			header := make([]byte, 4)
			if _, err := io.ReadFull(server, header); err != nil {
				return
			}
			host, port, err := readSOCKS5Target(server, header[3])
			if err != nil {
				return
			}
			if inspect != nil {
				inspect(host, port)
			}
			_ = writeAll(server, connectReply)
		}()
		return client, nil
	}
}

func readSOCKS5Target(reader io.Reader, addressType byte) (string, uint16, error) {
	var address []byte
	switch addressType {
	case 0x01:
		address = make([]byte, net.IPv4len)
	case 0x04:
		address = make([]byte, net.IPv6len)
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(reader, length); err != nil {
			return "", 0, err
		}
		address = make([]byte, int(length[0]))
	default:
		return "", 0, io.ErrUnexpectedEOF
	}
	if _, err := io.ReadFull(reader, address); err != nil {
		return "", 0, err
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(reader, port); err != nil {
		return "", 0, err
	}
	if addressType == 0x03 {
		return string(address), binary.BigEndian.Uint16(port), nil
	}
	return net.IP(address).String(), binary.BigEndian.Uint16(port), nil
}
