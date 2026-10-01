package client

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPumpHalfCloseDrainsReply(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	serverDone := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(time.Second))
		data, err := io.ReadAll(c)
		if err == nil {
			_, err = io.WriteString(c, "reply:"+string(data))
		}
		serverDone <- err
	}()
	c, err := Connect(l.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err = Pump(c, io.NopCloser(strings.NewReader("hello")), &output, true); err != nil {
		t.Fatal(err)
	}
	if output.String() != "reply:hello" {
		t.Fatal(output.String())
	}
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
}
func TestRemoteCloseCancelsBlockedInput(t *testing.T) {
	left, right := net.Pipe()
	input, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- Pump(left, input, io.Discard, false) }()
	_ = right.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked input leaked")
	}
}
func TestInputEOFClosesPeer(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	done := make(chan error, 1)
	go func() { done <- Pump(left, io.NopCloser(strings.NewReader("")), io.Discard, false) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("EOF hang")
	}
	if _, err := right.Read(make([]byte, 1)); err != io.EOF {
		t.Fatal(err)
	}
}

type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, errors.New("output failed") }
func TestOutputErrorClosesConnection(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	input, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- Pump(left, input, brokenOutput{}, false) }()
	_, _ = io.WriteString(right, "greeting")
	select {
	case err := <-done:
		if err == nil || err.Error() != "output failed" {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("output failure hang")
	}
}
func TestConnectRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	c, err := Connect(address, 100*time.Millisecond)
	if err == nil {
		_ = c.Close()
		t.Fatal("expected refusal")
	}
}
func TestScreenBoundAndControlFiltering(t *testing.T) {
	u := &terminal{}
	for i := 0; i < screenLines+10; i++ {
		u.add("line")
	}
	if len(u.lines) != screenLines {
		t.Fatal(len(u.lines))
	}
	if displayText("safe\x1b[31m\r\x00") != "safe[31m" {
		t.Fatal("control injection")
	}
}
