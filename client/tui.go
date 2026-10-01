package client

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jroimartin/gocui"
)

const screenLines = 2000

type terminal struct {
	lines      []string
	status     string
	send       chan string
	offset     int
	horizontal int
	draft      string
}

// TUI runs one terminal session. Network workers never access gocui views.
// Acknowledged Update calls preserve wire order and bound pending UI updates.
func TUI(socket net.Conn) error {
	defer socket.Close()
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return fmt.Errorf("terminal UI: %w", err)
	}
	defer g.Close()
	g.Cursor = true
	g.Highlight = true
	g.SelFgColor = gocui.ColorGreen
	u := &terminal{status: "Connected. Enter your name. Ctrl-C: exit | PgUp/PgDn: history | F7/F8: horizontal", send: make(chan string, 16)}
	g.SetManagerFunc(u.layout)
	if err = g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, func(*gocui.Gui, *gocui.View) error { return gocui.ErrQuit }); err != nil {
		return err
	}
	if err = g.SetKeybinding("input", gocui.KeyEnter, gocui.ModNone, u.submit); err != nil {
		return err
	}
	for key, delta := range map[gocui.Key]int{gocui.KeyPgup: 10, gocui.KeyPgdn: -10} {
		step := delta
		if err = g.SetKeybinding("", key, gocui.ModNone, func(*gocui.Gui, *gocui.View) error {
			u.offset += step
			if u.offset < 0 {
				u.offset = 0
			}
			if u.offset > len(u.lines)-1 {
				u.offset = len(u.lines) - 1
			}
			if u.offset < 0 {
				u.offset = 0
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for key, delta := range map[gocui.Key]int{gocui.KeyF7: -20, gocui.KeyF8: 20} {
		step := delta
		if err = g.SetKeybinding("", key, gocui.ModNone, func(*gocui.Gui, *gocui.View) error {
			u.horizontal += step
			if u.horizontal < 0 {
				u.horizontal = 0
			}
			if u.horizontal > 8192 {
				u.horizontal = 8192
			}
			return nil
		}); err != nil {
			return err
		}
	}
	done := make(chan struct{})
	var workers sync.WaitGroup
	update := func(fn func()) bool {
		ack := make(chan struct{})
		g.Update(func(*gocui.Gui) error { defer close(ack); fn(); return nil })
		select {
		case <-ack:
			return true
		case <-done:
			return false
		}
	}
	workers.Add(2)
	go func() {
		defer workers.Done()
		scan := bufio.NewScanner(socket)
		scan.Buffer(make([]byte, 4096), 16384)
		for scan.Scan() {
			line := displayText(scan.Text())
			if !update(func() { u.add(line) }) {
				return
			}
		}
		message := "Disconnected. Ctrl-C to exit."
		if scan.Err() != nil {
			message = "Connection ended: " + displayText(scan.Err().Error()) + " | Ctrl-C to exit"
		}
		update(func() { u.status = message })
	}()
	go func() {
		defer workers.Done()
		for {
			select {
			case text := <-u.send:
				_ = socket.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := fmt.Fprintln(socket, text); err != nil {
					_ = socket.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()
	err = g.MainLoop()
	close(done)
	_ = socket.Close()
	workers.Wait()
	if err == gocui.ErrQuit {
		return nil
	}
	return err
}

// Strip terminal control sequences even when connected to an unrelated server.
func displayText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || (r >= 127 && r <= 159) {
			return -1
		}
		return r
	}, s)
}
func (u *terminal) add(line string) {
	if len(u.lines) == screenLines {
		copy(u.lines, u.lines[1:])
		u.lines[len(u.lines)-1] = line
	} else {
		u.lines = append(u.lines, line)
	}
}
func (u *terminal) submit(_ *gocui.Gui, v *gocui.View) error {
	text := strings.TrimSuffix(v.Buffer(), "\n")
	if len(text) > 4096 {
		u.status = "Input too long: maximum 4096 bytes."
		return nil
	}
	select {
	case u.send <- text:
		u.add("> " + displayText(text)) // One local copy; the server does not echo it.
		u.offset = 0
		v.Clear()
		if err := v.SetCursor(0, 0); err != nil {
			return err
		}
		return v.SetOrigin(0, 0)
	default:
		u.status = "Output busy. Wait and press Enter again."
		return nil
	}
}
func (u *terminal) layout(g *gocui.Gui) error {
	w, h := g.Size()
	if w < 36 || h < 12 {
		if input, err := g.View("input"); err == nil {
			u.draft = strings.TrimSuffix(input.Buffer(), "\n")
		}
		for _, name := range []string{"header", "chat", "input", "status"} {
			_ = g.DeleteView(name)
		}
		if w > 2 && h > 2 {
			v, err := g.SetView("small", 0, 0, w-1, h-1)
			if err != nil && err != gocui.ErrUnknownView {
				return err
			}
			v.Clear()
			fmt.Fprintln(v, "Resize to 36x12 or larger.\nCtrl-C to exit.")
		}
		return nil
	}
	_ = g.DeleteView("small")
	for _, box := range []struct {
		name        string
		top, bottom int
	}{
		{"header", 0, 2}, {"chat", 3, h - 6}, {"input", h - 5, h - 3}, {"status", h - 2, h - 1},
	} {
		v, err := g.SetView(box.name, 0, box.top, w-1, box.bottom)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}
		switch box.name {
		case "header":
			v.Clear()
			v.FgColor = gocui.ColorGreen
			fmt.Fprint(v, "NET-LINK | GhostOfAstana | Discipline since 2008")
		case "chat":
			v.Title = " Chat · PgUp/PgDn · last 2000 lines (full history on server) "
			v.Clear()
			_, height := v.Size()
			end := len(u.lines) - u.offset
			start := end - height
			if start < 0 {
				start = 0
			}
			for _, line := range u.lines[start:end] {
				runes := []rune(line)
				if u.horizontal < len(runes) {
					fmt.Fprintln(v, string(runes[u.horizontal:]))
				} else {
					fmt.Fprintln(v)
				}
			}
		case "input":
			v.Title = " Name, message or /help "
			v.Editable = true
			if err == gocui.ErrUnknownView && u.draft != "" {
				for _, r := range u.draft {
					v.EditWrite(r)
				}
				u.draft = ""
			}
			v.Editor = gocui.EditorFunc(func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
				if (ch != 0 || key == gocui.KeySpace) && len(v.Buffer()) >= 4096 {
					return
				}
				gocui.DefaultEditor.Edit(v, key, ch, mod)
			})
		case "status":
			v.Frame = false
			v.Clear()
			fmt.Fprint(v, u.status)
		}
	}
	_, err := g.SetCurrentView("input")
	return err
}
