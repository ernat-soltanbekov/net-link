package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ernat-soltanbekov/net-link/client"
	"github.com/ernat-soltanbekov/net-link/server"
)

const usage = "[USAGE]: ./TCPChat $port\n"
const extendedHelp = `net-link — TCP chat, GhostOfAstana / Tomorrow School

  ./TCPChat [port]                 Listen (default 8989)
  ./TCPChat -l -p 2525 -s 127.0.0.1
  ./TCPChat -c [-N] [-v] [-w 5] host port
  ./TCPChat --tui [-w 5] host port
  ./TCPChat -c -z -v host port     Test whether a TCP port accepts connections

  -l        Listen mode (default)
  -p PORT   Listening port, 1–65535
  -s HOST   Local address to bind (default all interfaces)
  -c        Plain TCP client, compatible with nc
  --tui     gocui terminal client (36 columns × 12 rows minimum)
  -w SEC    Client connection timeout, 1–300 seconds (default 5)
  -N        Half-close output on input EOF, then wait for the peer to close
  -z        Connect and close without transmitting input (client only)
  -v        Connection diagnostics on stderr
  --log PATH  Server activity log (default logs/net-link.log)
  --help    Show this help

Commands: /nick NAME /join ROOM /rooms /who /profile /help /quit
`

type options struct {
	mode, host, port, bind, logPath string
	timeout                         time.Duration
	half, scan, verbose, help       bool
}

func number(s string, max int) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > max {
			return 0, false
		}
	}
	return n, n > 0
}
func parse(args []string) (options, bool) {
	o := options{mode: "server", port: "8989", timeout: 5 * time.Second, logPath: "logs/net-link.log"}
	pos := []string{}
	seen := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		if seen[a] {
			return o, false
		}
		seen[a] = true
		switch a {
		case "--help":
			o.help = true
		case "-c":
			o.mode = "client"
		case "--tui":
			o.mode = "tui"
		case "-l":
		case "-N":
			o.half = true
		case "-z":
			o.scan = true
		case "-v":
			o.verbose = true
		case "-p", "-s", "-w", "--log":
			i++
			if i == len(args) {
				return o, false
			}
			value := args[i]
			switch a {
			case "-p":
				o.port = value
			case "-s":
				o.bind = value
			case "--log":
				o.logPath = value
			case "-w":
				n, ok := number(value, 300)
				if !ok {
					return o, false
				}
				o.timeout = time.Duration(n) * time.Second
			}
		default:
			return o, false
		}
	}
	if o.help {
		return o, len(args) == 1
	}
	if seen["-c"] && seen["--tui"] {
		return o, false
	}
	if o.mode == "server" {
		if o.half || o.scan || seen["-w"] || len(pos) > 1 || (len(pos) == 1 && seen["-p"]) || o.logPath == "" {
			return o, false
		}
		if len(pos) == 1 {
			o.port = pos[0]
		}
	} else {
		if seen["-l"] || seen["-p"] || seen["-s"] || seen["--log"] || len(pos) != 2 {
			return o, false
		}
		if o.mode == "tui" && (o.half || o.scan) {
			return o, false
		}
		o.host, o.port = pos[0], pos[1]
		if o.host == "" {
			return o, false
		}
	}
	n, ok := number(o.port, 65535)
	o.port = fmt.Sprint(n)
	return o, ok
}

func run(args []string, input io.ReadCloser, output, diagnostic io.Writer) int {
	o, ok := parse(args)
	if !ok {
		_, _ = io.WriteString(output, usage)
		return 2
	}
	if o.help {
		_, _ = io.WriteString(output, extendedHelp)
		return 0
	}
	if o.mode != "server" {
		address := net.JoinHostPort(o.host, o.port)
		socket, err := client.Connect(address, o.timeout)
		if err != nil {
			fmt.Fprintln(diagnostic, "net-link:", err)
			return 1
		}
		if o.verbose {
			fmt.Fprintln(diagnostic, "Connected to", address)
		}
		if o.scan {
			_ = socket.Close()
			return 0
		}
		if o.mode == "tui" {
			err = client.TUI(socket)
		} else {
			err = client.Pump(socket, input, output, o.half)
		}
		if err != nil {
			fmt.Fprintln(diagnostic, "net-link:", err)
			return 1
		}
		return 0
	}
	// Parent directories are created only for the standard logs location.
	// An explicitly chosen path must already have a parent directory.
	if o.logPath == "logs/net-link.log" {
		if err := os.MkdirAll("logs", 0700); err != nil {
			fmt.Fprintln(diagnostic, "net-link:", err)
			return 1
		}
	}
	logFile, err := os.OpenFile(o.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(diagnostic, "net-link: open log:", err)
		return 1
	}
	defer logFile.Close()
	listener, err := net.Listen("tcp", net.JoinHostPort(o.bind, o.port))
	if err != nil {
		fmt.Fprintln(diagnostic, "net-link: listen:", err)
		return 1
	}
	s, err := server.Start(listener, server.Config{HistoryDir: "logs", Log: logFile})
	if err != nil {
		fmt.Fprintln(diagnostic, "net-link:", err)
		return 1
	}
	defer s.Close()
	fmt.Fprintln(output, "Listening on the port :"+o.port)
	if o.verbose {
		fmt.Fprintln(diagnostic, "Bound to", s.Addr(), "| activity log:", o.logPath)
	}
	if err := s.Wait(); err != nil {
		fmt.Fprintln(diagnostic, "net-link: accept:", err)
		return 1
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
