package catalog

import "testing"

func TestIsTrailer(t *testing.T) {
	t.Parallel()
	const mb = int64(1 << 20)
	cases := []struct {
		name     string
		file     string
		caption  string
		duration int32
		size     int64
		want     bool
	}{
		{"turkish fragman, no duration", "Recep İvedik 6 - Fragman (Official).mp4", "", 0, 15 * mb, true},
		{"official trailer", "Happy New Year Official Trailer.mp4", "", 0, 84 * mb, true},
		{"persian caption teaser", "video.mp4", "تیزر فیلم", 95, 0, true},
		{"dotted scene name", "Dune.Part.Two.Teaser.1080p.mkv", "", 70, 40 * mb, true},
		{"full film named trailer", "The.Trailer.2019.1080p.mkv", "", 5400, 1800 * mb, false},
		{"caption mentions trailer on a film", "Recep.Ivedik.6.mkv", "تریلر رو ببینید", 6555, 1455 * mb, false},
		{"no token, short", "clip.mp4", "", 60, 10 * mb, false},
		{"token, nothing known", "Fragman.mp4", "", 0, 0, false},
		{"token inside a word", "Trailerpark.Boys.S01E01.mkv", "", 1300, 200 * mb, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTrailer(c.file, c.caption, c.duration, c.size); got != c.want {
				t.Fatalf("IsTrailer(%q, %q, %d, %d) = %v, want %v", c.file, c.caption, c.duration, c.size, got, c.want)
			}
		})
	}
}
