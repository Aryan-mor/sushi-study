package config

import "testing"

func TestLoginPostChannel(t *testing.T) {
	t.Parallel()

	user, id, problems := loginPostChannel("exampleLoginChannel|-1009900000014")
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if user != "exampleLoginChannel" || id != -1009900000014 {
		t.Fatalf("got %s %d", user, id)
	}
}

func TestLoginPostChannelRejectsPositive(t *testing.T) {
	t.Parallel()

	if _, _, problems := loginPostChannel("x|123"); len(problems) == 0 {
		t.Fatal("positive channel id accepted")
	}
}

func TestLoginPostChannelRejectsEmpty(t *testing.T) {
	t.Parallel()

	if _, _, problems := loginPostChannel(""); len(problems) == 0 {
		t.Fatal("empty accepted")
	}
}
