package pairs_test

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/pairs"
)

const tok = "8645381957:AAHqf0Ex4mpleTokenOneAaBbCcDdEeFfGg"

func TestParseTwoFieldsKeepsTokenColon(t *testing.T) {
	t.Parallel()

	got, err := pairs.Parser{FieldNames: []string{"username", "token"}}.Parse(
		"API_BOTS", "alpha_bot|"+tok+",beta_bot|"+tok,
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 || got[0][1] != tok {
		t.Fatalf("got %+v, token colon was split", got)
	}
}

func TestParseThreeFields(t *testing.T) {
	t.Parallel()

	got, err := pairs.Parser{FieldNames: []string{"username", "token", "channel id"}}.Parse(
		"BACKUP_BOTS", "alpha_bot|"+tok+"|-1009900000007",
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got[0].Key() != "alpha_bot" || got[0][1] != tok || got[0][2] != "-1009900000007" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestParseRejectsWrongArity(t *testing.T) {
	t.Parallel()

	_, err := pairs.Parser{FieldNames: []string{"username", "token", "channel id"}}.Parse(
		"BACKUP_BOTS", "alpha_bot|"+tok,
	)
	if err == nil || !strings.Contains(err.Error(), "want 3") {
		t.Fatalf("error = %v, want arity", err)
	}
}

func TestParseRejectsColonFormat(t *testing.T) {
	t.Parallel()

	_, err := pairs.Parser{FieldNames: []string{"username", "token"}}.Parse(
		"API_BOTS", "alpha_bot:"+tok,
	)
	if err == nil || !strings.Contains(err.Error(), `"|"`) {
		t.Fatalf("error = %v, want a pointer at '|'", err)
	}
}

func TestErrorsNeverLeakTokens(t *testing.T) {
	t.Parallel()

	_, err := pairs.Parser{FieldNames: []string{"username", "token"}}.Parse(
		"API_BOTS", "alpha_bot|"+tok+",beta_bot",
	)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), tok) {
		t.Errorf("error leaked a token: %q", err)
	}
}
