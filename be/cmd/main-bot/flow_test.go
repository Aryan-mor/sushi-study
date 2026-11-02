package main

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

func TestCallbackScreenUsesTheMessageWhenPresent(t *testing.T) {
	t.Parallel()

	q := &tg.CallbackQuery{
		From:    &tg.User{ID: 9},
		Message: &tg.Message{ID: 7, Chat: tg.Chat{ID: 42}},
	}
	s := callbackScreen(q)
	if s.chatID != 42 || s.messageID != 7 {
		t.Fatalf("got chat=%d msg=%d, want 42/7", s.chatID, s.messageID)
	}
}

func TestCallbackScreenFallsBackToFromInAPrivateChat(t *testing.T) {
	t.Parallel()

	q := &tg.CallbackQuery{From: &tg.User{ID: 1000001}}
	s := callbackScreen(q)
	if s.chatID != 1000001 || s.messageID != 0 {
		t.Fatalf("got chat=%d msg=%d, want from-id and a new message", s.chatID, s.messageID)
	}
}

func TestCallbackScreenEmptyWhenNothingToAddress(t *testing.T) {
	t.Parallel()

	if got := callbackScreen(nil); got.chatID != 0 {
		t.Fatalf("nil query chat = %d", got.chatID)
	}
	if got := callbackScreen(&tg.CallbackQuery{}); got.chatID != 0 {
		t.Fatalf("empty query chat = %d", got.chatID)
	}
}
