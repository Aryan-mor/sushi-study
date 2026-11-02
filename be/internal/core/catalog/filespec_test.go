package catalog

import "testing"

func TestParseFileSpecFromSceneName(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("Shadow.and.Bone.S01E01.720p.WEB-DL.x265.10bit.Pahe.mkv")
	if s.Height != 720 || s.Codec != "x265" || s.Source != "WEB-DL" || !s.Bit10 {
		t.Fatalf("got %+v", s)
	}
	if s.Tech() != "720p x265 10bit WEB-DL" {
		t.Fatalf("tech %q", s.Tech())
	}
}

func TestParseFileSpecBare1080x265(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("1080 x265")
	if s.Height != 1080 || s.Codec != "x265" {
		t.Fatalf("got %+v", s)
	}
	if s.Tech() != "1080p x265" {
		t.Fatalf("tech %q", s.Tech())
	}
}

func TestParseFileSpecIgnoresYear(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("Disenchantment 2018 Farsi Dubbed")
	if s.Height != 0 {
		t.Fatalf("year became height: %+v", s)
	}
	if s.Delivery != DeliveryDub {
		t.Fatalf("dubbed not parsed: %+v", s)
	}
}

func TestParseFileSpecDelivery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		blob string
		want string
	}{
		{"The.Walking.Dead.S11E24.720p.WEB-DL.HardSub.DigiMoviez.mp4", DeliveryHardSub},
		{"Lucy.2014.480p.BluRay.HardSub.mkv", DeliveryHardSub},
		{"فیلم تنش شدید\nزیرنویس_چسبیده_فارسی\n720p", ""},
		{"فیلم تنش شدید\nهاردساب\n720p", DeliveryHardSub},
		{"Iron.Man.2.2010.720p.SoftSub.WEB-DL.x264-GRP.mkv", DeliverySoftSub},
		{"1080p SoftSub Persian", DeliverySoftSub},
		{"We.Bury.the.Dead.2024.1080p.WEB-DL.Farsi.Dubbed.mkv", DeliveryDub},
		{"1080p - dubbed (Persian) - WEB-DL", DeliveryDub},
		{"720p Dub WEB-DL", DeliveryDub},
		{"720p WEB-DL x265", ""},
	}
	for _, tc := range cases {
		if got := ParseFileSpec(tc.blob).Delivery; got != tc.want {
			t.Errorf("%q: delivery %q, want %q", tc.blob, got, tc.want)
		}
	}
}

func TestEnrichLabelAppendsHardSubFromFilename(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("Movie.2024.720p.WEB-DL.HardSub.mkv")
	got := s.EnrichLabel("720p WEB-DL")
	if got != "720p WEB-DL HardSub" {
		t.Fatalf("got %q", got)
	}
	if s.EnrichLabel("") != "720p WEB-DL HardSub" {
		t.Fatalf("empty enrich %q", s.EnrichLabel(""))
	}
	if s.EnrichLabel("720p HardSub WEB-DL") != "720p HardSub WEB-DL" {
		t.Fatalf("duplicate hardsub %q", s.EnrichLabel("720p HardSub WEB-DL"))
	}
}

func TestParseFileSpec4K(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("Movie.2024.4K.HDR.mkv")
	if s.Resolution() != "4K" {
		t.Fatalf("got %q", s.Resolution())
	}
}

func TestEnrichLabelAppendsCodec(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("foo.1080p.x265.mkv")
	got := s.EnrichLabel("1080p Dub WEB-DL")
	if got != "1080p Dub WEB-DL x265" {
		t.Fatalf("got %q", got)
	}
	if s.EnrichLabel("") != "1080p x265" {
		t.Fatalf("empty enrich %q", s.EnrichLabel(""))
	}
}

func TestParseFileSpecLowRes(t *testing.T) {
	t.Parallel()
	if h := ParseFileSpec("Movie.2024.360p.WEB-DL.mkv").Height; h != 360 {
		t.Fatalf("360p height %d", h)
	}
	if h := ParseFileSpec("clip.240p.mp4").Height; h != 240 {
		t.Fatalf("240p height %d", h)
	}
}

func TestParseFileSpecUnderscoreTokens(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("Spider_Man_Brand_New_Day_2026_720p_Hdrip_Duble_MyMoviz_video_converter.mp4")
	if s.Height != 720 || s.Source != "HDRip" || s.Delivery != DeliveryDub {
		t.Fatalf("underscore scene name: %+v", s)
	}
}

func TestParseFileSpecDuble(t *testing.T) {
	t.Parallel()
	s := ParseFileSpec("Spider.Man.Brand.New.Day.2026.720p.Hdrip.Duble.MyMoviz.mp4")
	if s.Delivery != DeliveryDub || s.Source != "HDRip" || s.Height != 720 {
		t.Fatalf("duble: %+v", s)
	}
}

func TestParseReleaseSpecBrandNewDay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, file, caption, wantLabel string
		wantHeight                     int32
	}{
		{
			"480p converter dub",
			"Spider_Man_Brand_New_Day_2026_720p_Hdrip_Duble_MyMoviz_video_converter.mp4",
			"دانلود فیلم جدید\n🎙 #دوبله_فارسی\n📤 کیفیت 480p (HDRip❗️)\n✂️ #بدون_سانسور",
			"480p HDRip Dub",
			480,
		},
		{
			"720p dub, polluted hardsub leftover in caption",
			"Spider.Man.Brand.New.Day.2026.720p.Hdrip.Duble.MyMoviz.mp4",
			"دانلود فیلم جدید\n🎙 #دوبله_فارسی\n📤 کیفیت 720p (HDRip❗️)\n✏️ زیرنویس فارسی چسبیده\n📤کیفیت : 720p*265",
			"720p HDRip Dub",
			720,
		},
		{
			"480p converter HDTS, چسبیده is not HardSub",
			"Spider_Man_Brand_New_Day_2026_720P_Hdts_MyMoviz_video_converter.mp4",
			"دانلود فیلم جدید\n📝 زیرنویس_فارسی_چسبیده\n📤 کیفیت 480p (پرده ای❗️)\n✂️ بدون_سانسور",
			"480p HDTS",
			480,
		},
		{
			"720p HDTS, چسبیده is not HardSub",
			"Spider.Man.Brand.New.Day.2026.720P.Hdts-MyMoviz.mp4",
			"دانلود فیلم جدید\n📝 زیرنویس_فارسی_چسبیده\n📤 کیفیت 720p (پرده ای❗️)\n✂️ بدون_سانسور",
			"720p HDTS",
			720,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := ParseReleaseSpec(tc.file, tc.caption)
			if s.Height != tc.wantHeight {
				t.Fatalf("height %d, want %d (%+v)", s.Height, tc.wantHeight, s)
			}
			if got := s.EnrichLabel(""); got != tc.wantLabel {
				t.Fatalf("label %q, want %q (%+v)", got, tc.wantLabel, s)
			}
		})
	}
}

func TestParseReleaseSpecCaptionFillsBareFilename(t *testing.T) {
	t.Parallel()
	s := ParseReleaseSpec("movie.mkv", "فیلم تنش شدید\nزیرنویس_چسبیده_فارسی\nکیفیت 720p")
	if s.Delivery != "" || s.Height != 720 {
		t.Fatalf("چسبیده must not become HardSub: %+v", s)
	}
	if !ClaimsPersianSub("زیرنویس_چسبیده_فارسی") {
		t.Fatal("want persian-sub claim")
	}
	if got := s.EnrichLabel(""); got != "720p" {
		t.Fatalf("label %q", got)
	}
}

func TestResolveDeliveryTracksBeatCaption(t *testing.T) {
	t.Parallel()
	claim := ParseFileSpec("Movie.2024.720p.WEB-DL.HardSub.mkv")
	if ResolveDelivery(claim, "", "fa") != DeliverySoftSub {
		t.Fatalf("srt in container must be SoftSub, got %q", ResolveDelivery(claim, "", "fa"))
	}
	if ResolveDelivery(claim, "fa", "") != DeliveryHardSub {
		t.Fatalf("explicit HardSub beats fa audio, got %q", ResolveDelivery(claim, "fa", ""))
	}
	chaspide := ParseReleaseSpec("movie.mkv", "زیرنویس_فارسی_چسبیده")
	if ResolveDelivery(chaspide, "", "") != "" {
		t.Fatalf("unprobed چسبیده is unknown, got %q", ResolveDelivery(chaspide, "", ""))
	}
	if ResolveDelivery(chaspide, "", "fa") != DeliverySoftSub {
		t.Fatalf("probed srt is SoftSub, got %q", ResolveDelivery(chaspide, "", "fa"))
	}
}
