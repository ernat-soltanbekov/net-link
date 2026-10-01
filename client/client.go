// Package client supplies the plain stream client and the optional gocui UI.
package client

import (
	"errors"
	"io"
	"net"
	"time"
)

func Connect(address string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", address, timeout)
}

// Pump owns socket and input. Closing input cancels a blocked terminal read when
// the peer disconnects. -N half-closes TCP output on EOF and drains the reply.
// Without -N, input EOF closes the whole connection like a short-lived nc client.
func Pump(socket net.Conn, input io.ReadCloser, output io.Writer, halfClose bool) error {
	defer socket.Close()
	defer input.Close()
	incoming := make(chan error, 1)
	outgoing := make(chan error, 1)
	go func() { _, err := io.Copy(output, socket); incoming <- err }()
	go func() { _, err := io.Copy(socket, input); outgoing <- err }()
	var result error
	select {
	case result = <-incoming:
		_ = input.Close()
		_ = socket.Close()
		<-outgoing
	case result = <-outgoing:
		if result == nil && halfClose {
			if tcp, ok := socket.(interface{ CloseWrite() error }); ok {
				result = tcp.CloseWrite()
			} else {
				result = errors.New("half-close requires a TCP connection")
			}
			if result == nil {
				result = <-incoming
				break
			}
		}
		_ = socket.Close()
		readErr := <-incoming
		if result == nil && !errors.Is(readErr, net.ErrClosed) && !errors.Is(readErr, io.ErrClosedPipe) {
			result = readErr
		}
	}
	return result
}
