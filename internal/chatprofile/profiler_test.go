package chatprofile

import "testing"

func TestPaceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		total   int
		minutes float64
		want    string
	}{
		{0, 0, "quiet"}, {12, 5, "quiet"}, {3, 1, "active"}, {10, 1, "active"},
		{2999, 1000, "quiet"}, {3001, 1000, "active"}, {9999, 1000, "active"}, {10001, 1000, "lively"},
		{1, 0, "lively"}, {-1, 1, "quiet"}, {1, -1, "lively"}, {15, 5, "active"},
	} {
		if got := Pace(tc.total, tc.minutes); got != tc.want {
			t.Errorf("Pace(%d,%g)=%s, want %s", tc.total, tc.minutes, got, tc.want)
		}
	}
	zero := 0.0
	if Pace(1, zero/zero) != "quiet" || Pace(1, 1/zero) != "quiet" {
		t.Fatal("non-finite elapsed time")
	}
}
func TestTopTalker(t *testing.T) {
	for _, tc := range []struct {
		counts map[string]int
		name   string
		n      int
	}{
		{nil, "", 0}, {map[string]int{"Yenlik": 8, "Lee": 7}, "Yenlik", 8},
		{map[string]int{"Zulu": 5, "Alpha": 5, "Low": 4}, "Alpha", 5},
		{map[string]int{"Zero": 0, "Negative": -3}, "", 0},
	} {
		for i := 0; i < 30; i++ {
			name, n := TopTalker(tc.counts)
			if name != tc.name || n != tc.n {
				t.Fatalf("got %q,%d", name, n)
			}
		}
	}
}
func TestSummary(t *testing.T) {
	if Summary(0, 0, nil) != "[System]: Session profile | pace: quiet | no messages yet\n" {
		t.Fatal("empty profile")
	}
	want := "[System]: Session profile | pace: active | top talker: Yenlik (8 msgs) | total: 15 msgs\n"
	if Summary(15, 2, map[string]int{"Yenlik": 8, "Lee": 7}) != want {
		t.Fatal("profile format")
	}
}
func FuzzPace(f *testing.F) {
	f.Add(15, 2.0)
	f.Add(0, 0.0)
	f.Add(3, 1.0)
	f.Add(10, 1.0)
	f.Fuzz(func(t *testing.T, n int, minutes float64) {
		got := Pace(n, minutes)
		if got != "quiet" && got != "active" && got != "lively" {
			t.Fatal(got)
		}
		if n > 0 && minutes > 0 {
			rate := float64(n) / minutes
			if (rate < 3 && got != "quiet") || (rate >= 3 && rate <= 10 && got != "active") || (rate > 10 && got != "lively") {
				t.Fatal(n, minutes, got)
			}
		}
	})
}
