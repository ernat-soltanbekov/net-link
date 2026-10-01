package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type peer struct {
	socket net.Conn
	reader *bufio.Reader
	t      *testing.T
}

func (p *peer) send(s string) {
	p.t.Helper()
	_ = p.socket.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintln(p.socket, s); err != nil {
		p.t.Fatal(err)
	}
}
func (p *peer) line() string {
	p.t.Helper()
	_ = p.socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	s, err := p.reader.ReadString('\n')
	if err != nil {
		p.t.Fatal(err)
	}
	return strings.TrimSuffix(s, "\n")
}
func (p *peer) until(part string) []string {
	p.t.Helper()
	result := []string{}
	for i := 0; i < 10000; i++ {
		s := p.line()
		result = append(result, s)
		if strings.Contains(s, part) {
			return result
		}
	}
	p.t.Fatal("missing", part)
	return nil
}
func (p *peer) profile() string {
	p.send("/profile")
	lines := p.until("Session profile |")
	return lines[len(lines)-1]
}
func connect(t *testing.T, s *Server) *peer {
	t.Helper()
	c, err := net.DialTimeout("tcp", s.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	p := &peer{c, bufio.NewReader(c), t}
	p.until("[ENTER YOUR NAME]:")
	return p
}
func join(t *testing.T, s *Server, name string) (*peer, []string) {
	t.Helper()
	p := connect(t, s)
	p.send(name)
	return p, p.until("[System]: Welcome " + name + " |")
}
func start(t *testing.T, cfg Config) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.HistoryDir = t.TempDir()
	s, err := Start(l, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func requireContains(t *testing.T, s, part string) {
	t.Helper()
	if !strings.Contains(s, part) {
		t.Fatalf("%q does not contain %q", s, part)
	}
}

type testClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *testClock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.value }
func (c *testClock) advance(d time.Duration) { c.mu.Lock(); c.value = c.value.Add(d); c.mu.Unlock() }

func TestChatHistoryNoEchoAndProfiles(t *testing.T) {
	clock := &testClock{value: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	s := start(t, Config{Now: clock.now})
	a, hello := join(t, s, "Yenlik")
	if hello[0] != "[System]: Session profile | pace: quiet | no messages yet" {
		t.Fatal(hello)
	}
	b, _ := join(t, s, "Lee")
	requireContains(t, a.line(), "[2026-01-01 12:00:00][System]: Lee has joined our chat.")
	clock.advance(time.Minute)
	a.send("hello")
	if b.line() != "[2026-01-01 12:01:00][Yenlik]:hello" {
		t.Fatal("wire format")
	}
	// Profile is an ordered barrier: no echo may appear before this reply.
	a.send("/profile")
	requireContains(t, a.line(), "pace: quiet | top talker: Yenlik (1 msgs) | total: 1 msgs")
	a.send("")
	a.send("   ")
	requireContains(t, a.profile(), "total: 1 msgs")
	c, history := join(t, s, "Third")
	if len(history) != 3 || history[0] != "[2026-01-01 12:01:00][Yenlik]:hello" || !strings.HasPrefix(history[1], "[System]: Session profile |") {
		t.Fatal(history)
	}
	a.until("Third has joined")
	b.until("Third has joined")
	b.send("from second")
	requireContains(t, a.line(), "[Lee]:from second")
	requireContains(t, c.line(), "[Lee]:from second")
	_ = b.socket.Close()
	a.until("Lee has left our chat.")
	c.until("Lee has left our chat.")
	a.send("still alive")
	requireContains(t, c.line(), "[Yenlik]:still alive")
	requireContains(t, c.profile(), "pace: active | top talker: Yenlik (2 msgs) | total: 3 msgs")
}

func TestRenameRoomsAndPrivateFloodWarning(t *testing.T) {
	clock := &testClock{value: time.Now()}
	s := start(t, Config{Now: clock.now})
	a, _ := join(t, s, "A")
	b, _ := join(t, s, "B")
	a.until("B has joined")
	for i := 0; i < 5; i++ {
		a.send(fmt.Sprint(i))
		b.until("[A]:")
	}
	a.send("/profile")
	requireContains(t, a.line(), "total: 5 msgs") // No warning at five.
	a.send("six")
	b.until("[A]:six")
	if a.line() != strings.TrimSuffix(FloodWarning, "\n") {
		t.Fatal("missing private flood warning")
	}
	b.send("/profile")
	requireContains(t, b.line(), "total: 6 msgs") // Warning not broadcast.
	clock.advance(30 * time.Second)
	a.send("after window")
	b.until("[A]:after window")
	a.send("/profile")
	requireContains(t, a.line(), "total: 7 msgs")
	a.send("/nick GhostOfAstana")
	a.until("A is now known as GhostOfAstana.")
	b.until("A is now known as GhostOfAstana.")
	requireContains(t, a.profile(), "top talker: GhostOfAstana (7 msgs)")
	a.send("/join dojo")
	a.until("no messages yet")
	a.until("room: dojo")
	b.until("GhostOfAstana has left")
	a.send("room secret")
	requireContains(t, a.profile(), "total: 1 msgs")
	b.send("/profile")
	requireContains(t, b.line(), "total: 7 msgs") // No room leakage.
	b.send("/join dojo")
	history := b.until("room: dojo")
	if len(history) != 3 || !strings.Contains(history[0], "room secret") || !strings.Contains(history[1], "total: 1 msgs") {
		t.Fatal(history)
	}
	a.until("B has joined")
	a.send("/rooms")
	requireContains(t, a.line(), "dojo (2), lobby (0)")
	a.send("/who")
	requireContains(t, a.line(), "B, GhostOfAstana")
}

func TestValidationAndTenSlotsIncludingHandshake(t *testing.T) {
	s := start(t, Config{})
	p := connect(t, s)
	p.send("  ")
	p.until("[ENTER YOUR NAME]:")
	p.send("System")
	p.until("[ENTER YOUR NAME]:")
	p.send("A")
	p.until("Welcome A |")
	q := connect(t, s)
	q.send("a")
	q.until("[ENTER YOUR NAME]:")
	q.send("B")
	q.until("Welcome B |")
	p.until("B has joined")
	p.send("bad\x1b[31m")
	p.until("Message rejected")
	p.send("/nick B")
	p.until("Name unavailable")
	p.send("/join ../escape")
	p.until("Usage: /join")
	requireContains(t, p.profile(), "no messages yet")
	pending := []*peer{}
	for i := 0; i < 8; i++ {
		pending = append(pending, connect(t, s))
	}
	conn, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	reply, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, string(reply), "Server is full (10 connections)")
	_ = pending[0].socket.Close()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		n := len(s.clients)
		s.mu.Unlock()
		if n == 9 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slot not released")
		}
		time.Sleep(time.Millisecond)
	}
	_, _ = join(t, s, "Replacement")
	p.until("Replacement has joined")
	q.until("Replacement has joined")
	p.send("survivor")
	q.until("[A]:survivor")
}

func TestOversizedLineAndHandshakeDeadline(t *testing.T) {
	s := start(t, Config{HandshakeTimeout: 80 * time.Millisecond})
	pending := connect(t, s)
	_ = pending.socket.SetReadDeadline(time.Now().Add(time.Second))
	_, err := pending.reader.ReadByte()
	if err == nil {
		t.Fatal("pending client stayed open")
	}
	a, _ := join(t, s, "A")
	b, _ := join(t, s, "B")
	a.until("B has joined")
	a.send(strings.Repeat("x", MaxMessageBytes))
	requireContains(t, b.line(), strings.Repeat("x", MaxMessageBytes))
	a.send(strings.Repeat("x", MaxMessageBytes+1))
	b.until("A has left")
	requireContains(t, b.profile(), "total: 1 msgs")
}

type failWriter struct {
	mu   sync.Mutex
	fail bool
	text strings.Builder
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errors.New("disk full (injected)")
	}
	return w.text.Write(p)
}
func TestPersistenceFailureDoesNotBroadcastOrCount(t *testing.T) {
	log := &failWriter{}
	s := start(t, Config{Log: log})
	a, _ := join(t, s, "A")
	b, _ := join(t, s, "B")
	a.until("B has joined")
	log.mu.Lock()
	log.fail = true
	log.mu.Unlock()
	a.send("uncommitted")
	a.until("Message not delivered")
	requireContains(t, b.profile(), "no messages yet")
	log.mu.Lock()
	log.fail = false
	log.mu.Unlock()
	a.send("saved")
	b.until("[A]:saved")
	_, history := join(t, s, "C")
	if len(history) != 3 || !strings.Contains(history[0], "saved") {
		t.Fatal(history)
	}
	log.mu.Lock()
	logs := log.text.String()
	log.mu.Unlock()
	requireContains(t, logs, "room=lobby")
	if strings.Contains(logs, "uncommitted") {
		t.Fatal(logs)
	}
	bytes, err := os.ReadFile(s.sessionDir + "/lobby.history")
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, string(bytes), "saved")
	if strings.Contains(string(bytes), "uncommitted") {
		t.Fatal(string(bytes))
	}
}

func TestHistoryWriteFailureAndStartupErrors(t *testing.T) {
	s := start(t, Config{})
	a, _ := join(t, s, "A")
	s.mu.Lock()
	_ = s.rooms["lobby"].history.Close()
	s.mu.Unlock()
	a.send("lost")
	a.until("Message not delivered")
	requireContains(t, a.profile(), "no messages yet")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Start(l, Config{HistoryDir: t.TempDir(), Log: &failWriter{fail: true}})
	if err == nil {
		t.Fatal("accepted broken logger")
	}
	if _, err = net.DialTimeout("tcp", l.Addr().String(), 50*time.Millisecond); err == nil {
		t.Fatal("listener leaked")
	}
}

func TestQueueOverflowAndWriteTimeoutAreIsolated(t *testing.T) {
	s := start(t, Config{WriteTimeout: 40 * time.Millisecond})
	left, right := net.Pipe()
	defer right.Close()
	c := &connection{socket: left, out: make(chan delivery, 1), done: make(chan struct{})}
	s.enqueue(c, delivery{text: "one"})
	s.enqueue(c, delivery{text: "two"})
	select {
	case <-c.done:
	default:
		t.Fatal("slow peer not disconnected")
	}
	left2, right2 := net.Pipe()
	defer right2.Close()
	c2 := &connection{socket: left2, out: make(chan delivery, 1), done: make(chan struct{})}
	c2.out <- delivery{text: "unread"}
	s.wg.Add(1)
	go s.write(c2)
	select {
	case <-c2.done:
	case <-time.After(time.Second):
		t.Fatal("write deadline did not fire")
	}
	a, _ := join(t, s, "Healthy")
	requireContains(t, a.profile(), "no messages yet")
}

func TestCloseUnblocksPendingReadsAndIdleWriters(t *testing.T) {
	s := start(t, Config{})
	for i := 0; i < 10; i++ {
		connect(t, s)
	}
	done := make(chan struct{})
	go func() { s.Close(); s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown deadlock")
	}
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentMessagesAndCompleteReplay(t *testing.T) {
	s := start(t, Config{QueueSize: 2048})
	peers := []*peer{}
	for i := 0; i < 4; i++ {
		p, _ := join(t, s, fmt.Sprintf("P%d", i))
		peers = append(peers, p)
	}
	const each = 150
	var senders sync.WaitGroup
	for i, p := range peers {
		senders.Add(1)
		go func(i int, p *peer) {
			defer senders.Done()
			for j := 0; j < each; j++ {
				_, _ = fmt.Fprintf(p.socket, "message-%d-%d\n", i, j)
			}
			_, _ = fmt.Fprintln(p.socket, "/profile")
		}(i, p)
	}
	// Drain all senders concurrently, including their private warning streams.
	var readers sync.WaitGroup
	fail := make(chan string, 4)
	for i, p := range peers {
		readers.Add(1)
		go func(i int, p *peer) {
			defer readers.Done()
			seen := map[string]bool{}
			for len(seen) < 3*each {
				_ = p.socket.SetReadDeadline(time.Now().Add(5 * time.Second))
				line, err := p.reader.ReadString('\n')
				if err != nil {
					fail <- err.Error()
					return
				}
				if strings.Contains(line, "]:message-") {
					if strings.Contains(line, fmt.Sprintf("[P%d]:", i)) || seen[line] {
						fail <- "echo or duplicate"
						return
					}
					seen[line] = true
				}
			}
		}(i, p)
	}
	senders.Wait()
	readers.Wait()
	close(fail)
	for err := range fail {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	_, history := join(t, s, "Archivist")
	if len(history) != 4*each+2 {
		t.Fatalf("replay: %d lines", len(history))
	}
	requireContains(t, history[len(history)-2], fmt.Sprintf("total: %d msgs", 4*each))
}

func TestRoomLimitAndInvalidCommands(t *testing.T) {
	s := start(t, Config{})
	a, _ := join(t, s, "A")
	for i := 1; i < MaxRooms; i++ {
		a.send(fmt.Sprintf("/join r%d", i))
		a.until(fmt.Sprintf("room: r%d |", i))
	}
	a.send("/join overflow")
	a.until("Room limit reached")
	for _, cmd := range []string{"/who extra", "/rooms extra", "/profile extra", "/quit extra", "/wat"} {
		a.send(cmd)
		requireContains(t, a.line(), "[System]:")
	}
	a.send("/help")
	a.until("Commands:")
	a.send("/quit")
	_ = a.socket.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := a.reader.ReadByte(); err == nil {
		t.Fatal("quit did not close")
	}
}

func TestFloodWindowSlides(t *testing.T) {
	c := &connection{}
	base := time.Now()
	for i := 0; i < 5; i++ {
		if c.flooding(base.Add(time.Duration(i) * 5 * time.Second)) {
			t.Fatal("early warning")
		}
	}
	if !c.flooding(base.Add(29 * time.Second)) {
		t.Fatal("sixth within window")
	}
	if c.flooding(base.Add(35 * time.Second)) {
		t.Fatal("expired messages counted")
	}
	for i := 0; i < 10000; i++ {
		c.flooding(base.Add(time.Hour))
		if len(c.recent) > 6 {
			t.Fatal("unbounded window")
		}
	}
}

func FuzzValidation(f *testing.F) {
	for _, s := range []string{"Alice", "", "System", "../escape", "hello\x1b[2J", "中文", "bad\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if validName(s) && (s == "" || len(s) > 64 || !validText(s) || strings.ContainsAny(s, "[]|:/\\")) {
			t.Fatal("invalid name accepted")
		}
		if validRoom(s) && (s == "" || len(s) > 32 || strings.ContainsAny(s, "./\\")) {
			t.Fatal("invalid room accepted")
		}
	})
}
