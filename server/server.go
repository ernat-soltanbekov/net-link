// Package server implements a line-oriented TCP chat compatible with nc.
package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ernat-soltanbekov/net-link/internal/chatprofile"
)

// Config controls resources, not the required ten-client limit.
// Now is injectable so rate boundaries can be tested without waiting minutes.
type Config struct {
	HistoryDir       string
	Log              io.Writer
	Now              func() time.Time
	HandshakeTimeout time.Duration
	WriteTimeout     time.Duration
	QueueSize        int
}

type room struct {
	name    string
	started time.Time
	history *os.File
	size    int64
	total   int
	counts  map[string]int
}

// A replay reads a fixed prefix of an append-only file. New messages go behind
// it in the same output queue, so replay and live traffic cannot cross or repeat.
type delivery struct {
	history *os.File
	size    int64
	text    string
}

type connection struct {
	socket   net.Conn
	out      chan delivery
	stopOnce sync.Once
	done     chan struct{}
	name     string
	room     *room
	counts   map[*room]int
	recent   []time.Time
}

func (c *connection) stop() { c.stopOnce.Do(func() { close(c.done); _ = c.socket.Close() }) }

// Server.mu owns membership, room counters, history appends, and queue ordering.
// No socket read or write runs while mu is held. A slow peer only fills its own
// bounded queue and is disconnected; it cannot stall other network writers.
type Server struct {
	mu         sync.Mutex
	listener   net.Listener
	cfg        Config
	logger     *log.Logger
	clients    map[*connection]bool
	rooms      map[string]*room
	closed     bool
	wg         sync.WaitGroup
	closeOnce  sync.Once
	acceptDone chan struct{}
	acceptErr  error
	sessionDir string
}

// Start takes ownership of listener. Close waits for all owned goroutines.
func Start(listener net.Listener, cfg Config) (*Server, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = io.Discard
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	if cfg.HistoryDir == "" {
		cfg.HistoryDir = "logs"
	}
	if err := os.MkdirAll(cfg.HistoryDir, 0700); err != nil {
		_ = listener.Close()
		return nil, err
	}
	dir, err := os.MkdirTemp(cfg.HistoryDir, "session-")
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	s := &Server{listener: listener, cfg: cfg, logger: log.New(cfg.Log, "", 0), clients: make(map[*connection]bool), rooms: make(map[string]*room), acceptDone: make(chan struct{}), sessionDir: dir}
	if _, err = s.newRoom("lobby"); err != nil {
		_ = listener.Close()
		_ = os.Remove(dir)
		return nil, err
	}
	if err = s.logger.Output(1, s.stamp()+" server started "+listener.Addr().String()); err != nil {
		_ = listener.Close()
		_ = s.rooms["lobby"].history.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("activity log: %w", err)
	}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) Addr() net.Addr { return s.listener.Addr() }
func (s *Server) Wait() error    { <-s.acceptDone; return s.acceptErr }
func (s *Server) stamp() string  { return "[" + s.cfg.Now().Format("2006-01-02 15:04:05") + "]" }

func (s *Server) newRoom(name string) (*room, error) {
	f, err := os.OpenFile(s.sessionDir+string(os.PathSeparator)+name+".history", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	r := &room{name: name, started: s.cfg.Now(), history: f, counts: make(map[string]int)}
	s.rooms[name] = r
	return r, nil
}

func (s *Server) accept() {
	defer s.wg.Done()
	defer close(s.acceptDone)
	for {
		socket, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			if !s.closed {
				s.acceptErr = err
			}
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = socket.Close()
			return
		}
		if len(s.clients) >= MaxConnections {
			s.mu.Unlock()
			// A tiny bounded refusal does not allocate a new client goroutine.
			_ = socket.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			_, _ = io.WriteString(socket, "[System]: Server is full (10 connections). Try again later.\n")
			_ = socket.Close()
			continue
		}
		c := &connection{socket: socket, out: make(chan delivery, s.cfg.QueueSize), counts: make(map[*room]int), done: make(chan struct{})}
		s.clients[c] = true // Pending names also consume a slot.
		s.wg.Add(2)
		c.out <- delivery{text: Welcome}
		s.mu.Unlock()
		go s.write(c)
		go s.read(c)
	}
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		_ = s.listener.Close()
		for c := range s.clients {
			c.stop()
		}
		s.mu.Unlock()
		s.wg.Wait()
		for _, r := range s.rooms {
			_ = r.history.Close()
		}
		_ = s.logger.Output(1, s.stamp()+" server stopped")
	})
}

// enqueue is called with mu held. Channels are never closed: disconnecting a
// reader cannot cause a concurrent broadcaster to panic on a closed channel.
func (s *Server) enqueue(c *connection, d delivery) {
	select {
	case c.out <- d:
	default:
		c.stop()
	}
}
func (s *Server) private(c *connection, text string) {
	s.enqueue(c, delivery{text: "[System]: " + text + "\n"})
}
func (s *Server) broadcast(r *room, except *connection, text string) {
	for peer := range s.clients {
		if peer != except && peer.room == r {
			s.enqueue(peer, delivery{text: text})
		}
	}
}
func (s *Server) event(r *room, except *connection, text string) {
	line := s.stamp() + "[System]: " + text + "\n"
	s.broadcast(r, except, line)
	if err := s.logger.Output(1, "room="+r.name+" "+strings.TrimSuffix(line, "\n")); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "net-link: activity log:", err)
	}
}
func (s *Server) profile(r *room) string {
	return chatprofile.Summary(r.total, s.cfg.Now().Sub(r.started).Minutes(), r.counts)
}
func (s *Server) replay(c *connection, r *room) {
	s.enqueue(c, delivery{history: r.history, size: r.size, text: s.profile(r) + "[System]: Welcome " + c.name + " | room: " + r.name + " | /help for commands\n"})
}
func (s *Server) nameAvailable(name string, self *connection) bool {
	for c := range s.clients {
		if c != self && strings.EqualFold(c.name, name) {
			return false
		}
	}
	return true
}

func (s *Server) read(c *connection) {
	defer s.wg.Done()
	defer func() {
		c.stop()
		s.mu.Lock()
		delete(s.clients, c)
		if c.room != nil && !s.closed {
			s.event(c.room, c, c.name+" has left our chat.")
		}
		s.mu.Unlock()
	}()
	_ = c.socket.SetReadDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
	scanner := bufio.NewScanner(c.socket)
	scanner.Buffer(make([]byte, 1024), MaxMessageBytes+2)
	for scanner.Scan() {
		line := scanner.Text()
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		if c.name == "" {
			name := strings.TrimSpace(line)
			if !validName(name) || !s.nameAvailable(name, c) {
				s.private(c, "Choose a unique, non-empty name (1–64 bytes; no controls, []|:/\\ or System).\n[ENTER YOUR NAME]:")
			} else {
				c.name, c.room = name, s.rooms["lobby"]
				s.replay(c, c.room)
				s.event(c.room, c, name+" has joined our chat.")
				_ = c.socket.SetReadDeadline(time.Time{})
			}
		} else if len(line) > MaxMessageBytes {
			s.mu.Unlock()
			return
		} else if !validText(line) {
			s.private(c, "Message rejected: control characters or invalid UTF-8.")
		} else if strings.HasPrefix(line, "/") {
			quit := s.command(c, line)
			if quit {
				s.mu.Unlock()
				return
			}
		} else if strings.TrimSpace(line) != "" {
			s.message(c, line)
		}
		s.mu.Unlock()
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		_ = s.logger.Output(1, s.stamp()+" input ended: "+err.Error())
	}
}

func (s *Server) message(c *connection, text string) {
	r := c.room
	line := s.stamp() + "[" + c.name + "]:" + text + "\n"
	// Persist before broadcasting/counting. Failure never creates phantom stats.
	n, err := r.history.WriteAt([]byte(line), r.size)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = s.logger.Output(1, "room="+r.name+" "+strings.TrimSuffix(line, "\n"))
	}
	if err != nil {
		_ = r.history.Truncate(r.size)
		s.private(c, "Message not delivered: unable to save history/log. Try again later.")
		return
	}
	r.size += int64(n)
	r.total++
	r.counts[c.name]++
	c.counts[r]++
	s.broadcast(r, c, line)
	if c.flooding(s.cfg.Now()) {
		s.enqueue(c, delivery{text: FloodWarning})
	}
}

// Refresh the deadline for each write, including large history replays.
type timedWriter struct {
	socket  net.Conn
	timeout time.Duration
}

func (w timedWriter) Write(p []byte) (int, error) {
	if err := w.socket.SetWriteDeadline(time.Now().Add(w.timeout)); err != nil {
		return 0, err
	}
	return w.socket.Write(p)
}
func (s *Server) write(c *connection) {
	defer s.wg.Done()
	defer c.stop()
	w := timedWriter{c.socket, s.cfg.WriteTimeout}
	for {
		select {
		case d := <-c.out:
			if d.history != nil {
				if _, err := io.Copy(w, io.NewSectionReader(d.history, 0, d.size)); err != nil {
					return
				}
			}
			if _, err := io.WriteString(w, d.text); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
