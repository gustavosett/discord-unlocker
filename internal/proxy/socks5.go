package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

func dialSOCKS5(
	ctx context.Context,
	dial DialContextFunc,
	endpoint Endpoint,
	targetAddress string,
) (net.Conn, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if dial == nil {
		return nil, errors.New("proxy: nil dialer")
	}
	targetHost, targetPortText, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return nil, fmt.Errorf("proxy: invalid SOCKS5 target: %w", err)
	}
	targetPort, err := strconv.ParseUint(targetPortText, 10, 16)
	if err != nil || targetPort == 0 {
		return nil, errors.New("proxy: invalid SOCKS5 target port")
	}
	target, err := socksTarget(targetHost, uint16(targetPort))
	if err != nil {
		return nil, err
	}

	connection, err := dial(ctx, "tcp", endpoint.Address())
	if err != nil {
		return nil, fmt.Errorf("proxy: dial SOCKS5 endpoint: %w", err)
	}
	success := false
	defer func() {
		if !success {
			connection.Close()
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("proxy: set SOCKS5 deadline: %w", err)
		}
	}

	cancelWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.SetDeadline(time.Now())
		case <-cancelWatch:
		}
	}()
	defer close(cancelWatch)

	if err := writeAll(connection, []byte{0x05, 0x01, 0x00}); err != nil {
		return nil, fmt.Errorf("proxy: write SOCKS5 greeting: %w", err)
	}
	methodReply := make([]byte, 2)
	if _, err := io.ReadFull(connection, methodReply); err != nil {
		return nil, fmt.Errorf("proxy: read SOCKS5 method: %w", err)
	}
	if methodReply[0] != 0x05 || methodReply[1] != 0x00 {
		return nil, fmt.Errorf("proxy: SOCKS5 endpoint rejected no-auth method")
	}

	request := make([]byte, 0, 3+len(target))
	request = append(request, 0x05, 0x01, 0x00)
	request = append(request, target...)
	if err := writeAll(connection, request); err != nil {
		return nil, fmt.Errorf("proxy: write SOCKS5 connect: %w", err)
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(connection, header); err != nil {
		return nil, fmt.Errorf("proxy: read SOCKS5 reply: %w", err)
	}
	if header[0] != 0x05 || header[2] != 0x00 {
		return nil, errors.New("proxy: malformed SOCKS5 reply")
	}
	if header[1] != 0x00 {
		return nil, fmt.Errorf("proxy: SOCKS5 connect failed with code %d", header[1])
	}
	if err := consumeSOCKSAddress(connection, header[3]); err != nil {
		return nil, err
	}

	success = true
	return connection, nil
}

func socksTarget(host string, port uint16) ([]byte, error) {
	if port == 0 || strings.ContainsRune(host, '\x00') {
		return nil, errors.New("proxy: invalid SOCKS5 target")
	}
	result := make([]byte, 0, 1+len(host)+2)
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			result = append(result, 0x01)
			result = append(result, ipv4...)
		} else {
			result = append(result, 0x04)
			result = append(result, ip.To16()...)
		}
	} else {
		host = strings.TrimSuffix(strings.ToLower(host), ".")
		if !validDNSName(host) || len(host) > 255 {
			return nil, errors.New("proxy: invalid SOCKS5 target hostname")
		}
		result = append(result, 0x03, byte(len(host)))
		result = append(result, host...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	result = append(result, portBytes...)
	return result, nil
}

func consumeSOCKSAddress(reader io.Reader, addressType byte) error {
	var addressLength int
	switch addressType {
	case 0x01:
		addressLength = net.IPv4len
	case 0x04:
		addressLength = net.IPv6len
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(reader, length); err != nil {
			return fmt.Errorf("proxy: read SOCKS5 bound hostname length: %w", err)
		}
		if length[0] == 0 {
			return errors.New("proxy: empty SOCKS5 bound hostname")
		}
		addressLength = int(length[0])
	default:
		return errors.New("proxy: unknown SOCKS5 bound address type")
	}
	addressAndPort := make([]byte, addressLength+2)
	if _, err := io.ReadFull(reader, addressAndPort); err != nil {
		return fmt.Errorf("proxy: read SOCKS5 bound address: %w", err)
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func validDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}
