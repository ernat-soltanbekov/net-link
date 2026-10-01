package server

import (
	"strings"
	"time"
)

const MaxMessageBytes = 4096
const MaxConnections = 10
const MaxRooms = 32
const FloodWarning = "[System]: You are sending messages too fast. Please slow down.\n"
const Welcome = "Welcome to TCP-Chat!\n" +
	"         _nnnn_\n" +
	"        dGGGGMMb\n" +
	"       @p~qp~~qMb\n" +
	"       M|@||@) M|\n" +
	"       @,----.JM|\n" +
	"      JS^\\__/  qKL\n" +
	"     dZP        qKRb\n" +
	"    dZP          qKKb\n" +
	"   fZP            SMMb\n" +
	"   HZM            MMMM\n" +
	"   FqM            MMMM\n" +
	" __| \".        |\\dS\"qML\n" +
	" |    `.       | `' \\Zq\n" +
	"_)      \\.___.,|     .'\n" +
	"\\____   )MMMMMP|   .'\n" +
	"     `-'       `--'\n" +
	"[ENTER YOUR NAME]:\n"

// Отсекаем управляющие символы и некорректный UTF-8, чтобы исключить внедрение команд
// терминала и поддельных строк журнала. Цикл range заменяет некорректный UTF-8 на U+FFFD;
// запрет этой руны также запрещает явно введённый символ U+FFFD.
func validText(s string) bool {
	for _, r := range s {
		if r < 32 || (r >= 127 && r <= 159) || r == '\ufffd' || r == '\u2028' || r == '\u2029' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			return false
		}
	}
	return true
}

func validName(s string) bool {
	return s != "" && len(s) <= 64 && validText(s) && !strings.ContainsAny(s, "[]|:/\\") && !strings.EqualFold(s, "System")
}

func validRoom(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Для обнаружения шестого сообщения за 30 секунд достаточно хранить шесть отметок времени.
// Пустые сообщения и команды в эту функцию не попадают.
func (c *connection) flooding(now time.Time) bool {
	kept := c.recent[:0]
	for _, t := range c.recent {
		if now.Sub(t) < 30*time.Second {
			kept = append(kept, t)
		}
	}
	c.recent = append(kept, now)
	if len(c.recent) > 6 {
		c.recent = c.recent[1:]
	}
	return len(c.recent) > 5
}
