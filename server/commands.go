package server

import (
	"fmt"
	"strings"
)

const help = "Commands: /nick NAME, /join ROOM, /rooms, /who, /profile, /help, /quit. Rooms use a-z, 0-9, - and _."

// command использует ту же блокировку, что и обработка сообщений: смена комнаты
// выполняется целиком, поэтому сообщение не попадёт в чужую комнату во время перехода.
func (s *Server) command(c *connection, line string) bool {
	parts := strings.SplitN(line, " ", 2)
	arg := ""
	if len(parts) == 2 {
		arg = strings.TrimSpace(parts[1])
	}
	switch parts[0] {
	case "/quit":
		if arg != "" {
			s.private(c, "Usage: /quit")
			return false
		}
		return true
	case "/nick":
		if !validName(arg) || !s.nameAvailable(arg, c) {
			s.private(c, "Name unavailable or invalid.")
			break
		}
		old := c.name
		if old == arg {
			s.private(c, "Your name is already "+arg+".")
			break
		}
		for r, n := range c.counts {
			r.counts[old] -= n
			if r.counts[old] == 0 {
				delete(r.counts, old)
			}
			r.counts[arg] += n
		}
		c.name = arg
		s.event(c.room, nil, old+" is now known as "+arg+".")
	case "/join":
		if !validRoom(arg) {
			s.private(c, "Usage: /join ROOM (1–32 lowercase letters/digits/-/_).")
			break
		}
		if arg == c.room.name {
			s.private(c, "You are already in "+arg+".")
			break
		}
		r := s.rooms[arg]
		if r == nil {
			if len(s.rooms) >= MaxRooms {
				s.private(c, "Room limit reached (32). Use /rooms.")
				break
			}
			var err error
			r, err = s.newRoom(arg)
			if err != nil {
				s.private(c, "Unable to create room history.")
				break
			}
		}
		s.event(c.room, c, c.name+" has left our chat.")
		c.room = r
		s.replay(c, r)
		s.event(r, c, c.name+" has joined our chat.")
	case "/rooms":
		if arg != "" {
			s.private(c, "Usage: /rooms")
			break
		}
		names := []string{}
		for name, r := range s.rooms {
			count := 0
			for peer := range s.clients {
				if peer.room == r {
					count++
				}
			}
			names = insert(names, fmt.Sprintf("%s (%d)", name, count))
		}
		s.private(c, "Rooms: "+strings.Join(names, ", "))
	case "/who":
		if arg != "" {
			s.private(c, "Usage: /who")
			break
		}
		names := []string{}
		for peer := range s.clients {
			if peer.room == c.room {
				names = insert(names, peer.name)
			}
		}
		s.private(c, "Room "+c.room.name+": "+strings.Join(names, ", "))
	case "/profile":
		if arg != "" {
			s.private(c, "Usage: /profile")
			break
		}
		s.enqueue(c, delivery{text: s.profile(c.room)})
	case "/help":
		s.private(c, help)
	default:
		s.private(c, "Unknown command. "+help)
	}
	return false
}

// Пакет sort отсутствует в списке разрешённых. В списках не более 32 элементов.
func insert(items []string, item string) []string {
	items = append(items, item)
	for i := len(items) - 1; i > 0 && items[i] < items[i-1]; i-- {
		items[i], items[i-1] = items[i-1], items[i]
	}
	return items
}
