package tg

import "testing"

func TestErrorIsBotToBotDenied(t *testing.T) {
	t.Parallel()

	denied := []struct {
		code int
		desc string
	}{
		{400, "Bad Request: USER_BOT_TO_BOT_DISABLED"}, // observed live 2026-09-02
		{403, "Forbidden: bots can't send messages to bots"},
		{403, "Forbidden: bot can't send messages to bots"},
		{403, "Forbidden: bots can't send messages to other bots"},
		{403, "Forbidden: BOT TO BOT communication is disabled"},
	}
	for _, c := range denied {
		if !(&Error{Code: c.code, Description: c.desc}).IsBotToBotDenied() {
			t.Errorf("want IsBotToBotDenied for %d %q", c.code, c.desc)
		}
	}

	notDenied := []struct {
		code int
		desc string
	}{
		{403, "Forbidden: bot was blocked by the user"},
		{403, "Forbidden: user is deactivated"},
		{400, "Bad Request: chat not found"},
		{401, "Unauthorized"},
	}
	for _, c := range notDenied {
		if (&Error{Code: c.code, Description: c.desc}).IsBotToBotDenied() {
			t.Errorf("want !IsBotToBotDenied for %d %q", c.code, c.desc)
		}
	}
}

func TestErrorIsForbidden(t *testing.T) {
	t.Parallel()

	if !(&Error{Code: 403, Description: "Forbidden: anything"}).IsForbidden() {
		t.Error("want IsForbidden for a 403")
	}
	if (&Error{Code: 400, Description: "Bad Request"}).IsForbidden() {
		t.Error("want !IsForbidden for a 400")
	}
}

func TestErrorIsNotModified(t *testing.T) {
	t.Parallel()

	noop := &Error{Code: 400, Description: "Bad Request: message is not modified"}
	if !noop.IsNotModified() {
		t.Error("want IsNotModified for the Telegram no-op edit")
	}
	if dropNotModified(noop) != nil {
		t.Error("want dropNotModified to swallow the no-op 400")
	}
	other := &Error{Code: 400, Description: "Bad Request: chat not found"}
	if other.IsNotModified() {
		t.Error("want !IsNotModified for a different 400")
	}
	if dropNotModified(other) == nil {
		t.Error("want dropNotModified to keep a real 400")
	}
}
