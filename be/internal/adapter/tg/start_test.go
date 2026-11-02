package tg

import "testing"

func TestStartURL(t *testing.T) {
	t.Parallel()

	if got := StartURL("exampleMainBot", "wl_m_550"); got != "https://t.me/exampleMainBot?start=wl_m_550" {
		t.Errorf("StartURL = %q", got)
	}
	if got := StartURL("@exampleMainBot", "m_550"); got != "https://t.me/exampleMainBot?start=m_550" {
		t.Errorf("StartURL with @ = %q", got)
	}
	if StartURL("", "wl_m_550") != "" {
		t.Error("empty username must not mint a URL")
	}
	if StartURL("bot", "") != "" {
		t.Error("empty payload must not mint a URL")
	}
}
