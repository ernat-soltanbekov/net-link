// Пакет chatprofile содержит чистые функции статистики чата, проверяемые независимо.
package chatprofile

import "fmt"

// Pace классифицирует скорость без округления, включая граничные значения 3 и 10.
// Если сообщения уже есть, а прошедшее время равно нулю, активность считается мгновенной: lively.
func Pace(total int, elapsedMinutes float64) string {
	if total <= 0 || elapsedMinutes != elapsedMinutes {
		return "quiet"
	}
	if elapsedMinutes <= 0 {
		return "lively"
	}
	rate := float64(total) / elapsedMinutes
	if rate < 3 {
		return "quiet"
	}
	if rate <= 10 {
		return "active"
	}
	return "lively"
}

// TopTalker при равенстве счётчиков выбирает первое имя в порядке сравнения строк Go,
// независимо от порядка обхода словаря.
func TopTalker(counts map[string]int) (name string, count int) {
	for candidate, n := range counts {
		if n > count || (n == count && n > 0 && candidate < name) {
			name, count = candidate, n
		}
	}
	return
}

func Summary(total int, minutes float64, counts map[string]int) string {
	if total == 0 {
		return "[System]: Session profile | pace: quiet | no messages yet\n"
	}
	name, count := TopTalker(counts)
	return fmt.Sprintf("[System]: Session profile | pace: %s | top talker: %s (%d msgs) | total: %d msgs\n", Pace(total, minutes), name, count, total)
}
