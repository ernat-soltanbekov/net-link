// Пакет client предоставляет обычный потоковый клиент и дополнительный интерфейс gocui.
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

// Pump управляет ресурсами socket и input. При отключении другой стороны закрытие
// input прерывает заблокированное чтение из терминала. С флагом -N конец ввода (EOF)
// закрывает только отправляющую сторону TCP, после чего клиент дочитывает ответ.
// Без -N конец ввода закрывает соединение целиком, как при коротком сеансе nc.
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
