package main

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
)

func TestAwaitingTitle(t *testing.T) {
	t.Parallel()

	on := &bot{personal: &personal.Service{}}
	on.rememberTitleWait(10, 42)

	if id, ok := on.awaitingTitle(10, inbound{rest: "Trip"}); !ok || id != 42 {
		t.Errorf("remembered prompt: got %d, %v; want 42, true", id, ok)
	}
	// After a restart nothing is remembered: a reply still names the newest waiting video.
	if id, ok := on.awaitingTitle(11, inbound{rest: "Trip", replyToID: 900}); !ok || id != 0 {
		t.Errorf("reply after restart: got %d, %v; want 0, true", id, ok)
	}
	// A plain message that is not a reply is not a title.
	if _, ok := on.awaitingTitle(11, inbound{rest: "hello"}); ok {
		t.Error("an ordinary message was taken as a title")
	}
	// Commands are never titles.
	if _, ok := on.awaitingTitle(10, inbound{cmd: "start"}); ok {
		t.Error("a command was taken as a title")
	}
	// Intake off: nothing is a title.
	off := &bot{}
	if _, ok := off.awaitingTitle(11, inbound{rest: "Trip", replyToID: 900}); ok {
		t.Error("a reply was taken as a title with intake off")
	}
}
