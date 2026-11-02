package botpool_test

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/botpool"
)

const (
	tok1 = "8645381957:AAHqf0Ex4mpleTokenOneAaBbCcDdEeFfGg"
	tok2 = "8271796073:AAHqf0Ex4mpleTokenTwoAaBbCcDdEeFfGg"
)

func TestParseKeepsTokensWhole(t *testing.T) {
	t.Parallel()

	p, err := botpool.Parse("API_BOTS", "alpha_bot|"+tok1+",beta_bot|"+tok2)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Len() != 2 {
		t.Fatalf("Len = %d, want 2", p.Len())
	}

	got := p.Bots()
	if got[0].Username != "alpha_bot" || got[0].Token != tok1 {
		t.Errorf("entry 1 = %q / %q", got[0].Username, got[0].Token)
	}
	if got[1].Token != tok2 {
		t.Errorf("entry 2 token = %q, want the full token including its colon", got[1].Token)
	}
}

func TestParseTolerantOfHumanFormatting(t *testing.T) {
	t.Parallel()

	p, err := botpool.Parse("API_BOTS", "  @alpha_bot | "+tok1+" ,\tbeta_bot|"+tok2+"  ")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if u := p.Usernames(); u[0] != "alpha_bot" || u[1] != "beta_bot" {
		t.Errorf("usernames = %v, want the '@' and the padding stripped", u)
	}
}

func TestParseRejectsAndNamesTheCulprit(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		raw       string
		wantInErr string
	}{
		"empty secret":       {"", "no entries"},
		"blank entry":        {"alpha_bot|" + tok1 + ",", "entry 2 is empty"},
		"missing separator":  {"alpha_bot|" + tok1 + ",beta_bot", "entry 2 has no \"|\""},
		"empty username":     {"|" + tok1, "entry 1 has an empty username"},
		"empty token":        {"alpha_bot|", "entry 1 (alpha_bot) has an empty token"},
		"token not a token":  {"alpha_bot|not-a-token", "entry 1 (alpha_bot) does not look like a bot token"},
		"duplicate username": {"alpha_bot|" + tok1 + ",Alpha_Bot|" + tok2, "appears twice, at entries 1 and 2"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := botpool.Parse("API_BOTS", tc.raw)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.wantInErr)
			}
			if !strings.Contains(err.Error(), "API_BOTS") {
				t.Errorf("error = %q, want it to name the secret", err)
			}
		})
	}
}

func TestErrorsNeverLeakTokens(t *testing.T) {
	t.Parallel()

	_, err := botpool.Parse("API_BOTS", "alpha_bot|"+tok1+",beta_bot")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), tok1) {
		t.Errorf("error leaked a token: %q", err)
	}
}

func TestStringIsSafeToLog(t *testing.T) {
	t.Parallel()

	p, err := botpool.Parse("API_BOTS", "alpha_bot|"+tok1)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s := p.String(); strings.Contains(s, tok1) {
		t.Errorf("String() leaked a token: %q", s)
	}
}

func TestByUsername(t *testing.T) {
	t.Parallel()

	p, err := botpool.Parse("DELIVERY_BOTS", "alpha_bot|"+tok1+",beta_bot|"+tok2)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if b, ok := p.ByUsername("@ALPHA_BOT"); !ok || b.Token != tok1 {
		t.Errorf("ByUsername(@ALPHA_BOT) = %+v, %v", b, ok)
	}
	if _, ok := p.ByUsername("gamma_bot"); ok {
		t.Error("ByUsername found a bot that is not in the pool")
	}
}

func TestOrderIsStable(t *testing.T) {
	t.Parallel()

	raw := "c_bot|" + tok1 + ",a_bot|" + tok2 + ",b_bot|" + tok1
	p, err := botpool.Parse("API_BOTS", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"c_bot", "a_bot", "b_bot"}
	for i, u := range p.Usernames() {
		if u != want[i] {
			t.Fatalf("usernames = %v, want %v (secret order, not sorted)", p.Usernames(), want)
		}
	}
}

func TestParseBound(t *testing.T) {
	t.Parallel()

	p, err := botpool.ParseBound("BACKUP_BOTS",
		"alpha_bot|"+tok1+"|-1009900000007,beta_bot|"+tok2+"|-1009900000008")
	if err != nil {
		t.Fatalf("ParseBound: %v", err)
	}
	if p.Len() != 2 {
		t.Fatalf("Len = %d", p.Len())
	}
	got := p.Bots()
	if got[0].ChannelID != -1009900000007 || got[0].Token != tok1 {
		t.Errorf("entry 1 = %+v", got[0])
	}
	if s := p.String(); strings.Contains(s, tok1) {
		t.Errorf("String() leaked a token: %q", s)
	}
}

func TestParseBoundRejectsPositiveChannel(t *testing.T) {
	t.Parallel()

	_, err := botpool.ParseBound("BACKUP_BOTS", "alpha_bot|"+tok1+"|100123")
	if err == nil || !strings.Contains(err.Error(), "channel ids are negative") {
		t.Fatalf("error = %v", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add("alpha_bot|" + tok1)
	f.Add("alpha_bot|" + tok1 + ",beta_bot|" + tok2)
	f.Add(",,,")
	f.Add("||||")
	f.Add("@a| 1:2 |,@b|3:4")

	f.Fuzz(func(t *testing.T, raw string) {
		p, err := botpool.Parse("API_BOTS", raw)
		if err != nil {
			return
		}
		for _, b := range p.Bots() {
			if b.Username == "" || b.Token == "" {
				t.Fatalf("accepted an entry with an empty field: %+v (from %q)", b, raw)
			}
			if !strings.Contains(b.Token, ":") {
				t.Fatalf("accepted a token with no colon: %q (from %q)", b.Token, raw)
			}
		}
		if p.Len() == 0 {
			t.Fatalf("accepted %q as a pool with no bots", raw)
		}
	})
}
