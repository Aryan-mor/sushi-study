package main

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
)

func TestDirectMessageIgnoresGroups(t *testing.T) {
	t.Parallel()
	if !directMessage(42) {
		t.Fatal("a private chat id is positive")
	}
	if directMessage(-1009900000000) || directMessage(-123) {
		t.Fatal("group and supergroup ids must not get a /start reply")
	}
}

func TestParseInbound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		text   string
		cmd    string
		rest   string
		token  bool
		gemini bool
	}{
		{name: "bare start", text: "/start", cmd: "start"},
		{name: "start bot deep link", text: "/start bot", cmd: "start", rest: "bot"},
		{name: "start ai_key", text: "/start ai_key", cmd: "start", rest: "ai_key"},
		{name: "start download", text: "/start download", cmd: "start", rest: "download"},
		{name: "start appcode nonce", text: "/start ac_k7x2", cmd: "start", rest: "ac_k7x2"},
		{name: "start with bot suffix", text: "/start@exampleMainBot", cmd: "start"},
		{name: "start with token payload is scrubbed", text: "/start 123456:ABCDEFGHIJKLMNOPQRSTUV", cmd: "start", rest: ""},
		{name: "login command", text: "/login", cmd: "login"},
		{name: "settings command", text: "/settings", cmd: "settings"},
		{name: "admin command", text: "/admin", cmd: "admin"},
		{name: "index command", text: "/index", cmd: "index"},
		{name: "help", text: "/HELP", cmd: "help"},
		{name: "human prose", text: "hey there", rest: "hey there"},
		{name: "pasted token", text: "123456789:AAThisLooksLikeATelegramTok", token: true, rest: "123456789:AAThisLooksLikeATelegramTok"},
		{name: "pasted gemini key", text: "AIzaSyDummyKeyForTestsOnly1234567890AB", gemini: true, rest: "AIzaSyDummyKeyForTestsOnly1234567890AB"},
		{name: "pasted new google api key", text: "AQ.Ab8RN6DummyKeyForTestsOnly1234567890ABCD", gemini: true, rest: "AQ.Ab8RN6DummyKeyForTestsOnly1234567890ABCD"},
		{name: "initbot leftover", text: "/initbot", cmd: "initbot"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseInbound(tc.text)
			if got.token != tc.token {
				t.Fatalf("token = %v, want %v", got.token, tc.token)
			}
			if got.gemini != tc.gemini {
				t.Fatalf("gemini = %v, want %v", got.gemini, tc.gemini)
			}
			if got.cmd != tc.cmd {
				t.Fatalf("cmd = %q, want %q", got.cmd, tc.cmd)
			}
			if got.rest != tc.rest {
				t.Fatalf("rest = %q, want %q", got.rest, tc.rest)
			}
		})
	}
}

func TestIsAppCodeStart(t *testing.T) {
	t.Parallel()

	if !isAppCodeStart("bot") || !isAppCodeStart("ac_k7x2") || !isAppCodeStart("appcode") {
		t.Fatal("expected app-code payloads")
	}
	if isAppCodeStart("") || isAppCodeStart("login") || isAppCodeStart("help") {
		t.Fatal("rejected payloads must not match")
	}
}

func TestIsDeleteAccountStart(t *testing.T) {
	t.Parallel()
	if !isDeleteAccountStart("delete_account") || !isDeleteAccountStart("DELETE_ACCOUNT") {
		t.Fatal("expected delete_account payloads")
	}
	if isDeleteAccountStart("") || isDeleteAccountStart("login") || isDeleteAccountStart("ac_x") {
		t.Fatal("rejected payloads must not match")
	}
}

func TestIsDownloadStart(t *testing.T) {
	t.Parallel()
	if !isDownloadStart("download") || !isDownloadStart("DOWNLOAD") || !isDownloadStart(" download ") {
		t.Fatal("expected download payloads")
	}
	if isDownloadStart("") || isDownloadStart("login") || isDownloadStart("bot") || isDownloadStart("app_m_550") {
		t.Fatal("rejected payloads must not match")
	}
}

func TestIsAiKeyStart(t *testing.T) {
	t.Parallel()
	if !isAiKeyStart("ai_key") || !isAiKeyStart("set_ai_key") || !isAiKeyStart("AI_KEY") {
		t.Fatal("expected ai_key payloads")
	}
	if isAiKeyStart("") || isAiKeyStart("login") || isAiKeyStart("bot") {
		t.Fatal("rejected payloads must not match")
	}
}

func TestLooksLikeGeminiKey(t *testing.T) {
	t.Parallel()
	good := "AIzaSyDummyKeyForTestsOnly1234567890AB"
	aq := "AQ.Ab8RN6DummyKeyForTestsOnly1234567890ABCD"
	if !looksLikeGeminiKey(good) {
		t.Fatal("expected a typical AI Studio key")
	}
	if !looksLikeGeminiKey(aq) {
		t.Fatal("expected a newer Google API key (AQ. prefix)")
	}
	if looksLikeGeminiKey("123456789:AAThisLooksLikeATelegramTok") {
		t.Fatal("a bot token must not look like a Gemini key")
	}
	if looksLikeGeminiKey("AIza short") || looksLikeGeminiKey("hello") || looksLikeGeminiKey("AIza\nSyxxxxxxxxxxxxxxxxxxxxxxxxxxx") || looksLikeGeminiKey("AQ.short") {
		t.Fatal("rejected shapes must not match")
	}
	if enroll.LooksLikeToken(good) || enroll.LooksLikeToken(aq) {
		t.Fatal("a Gemini key must not match enroll.LooksLikeToken")
	}
}
