package main

// Copy lives here in both languages until there is enough of it to deserve an i18n bundle
// (R-I18N-10). The tone is the thing to preserve, same as main-bot's: warm, short, second person.
// Per R-VOICE-1, nothing here may name a bot handle, an assignment, a binding, a provider, a pool
// or an id.
type message struct{ en, fa string }

func (m message) pick(locale string) string {
	if locale == "fa" {
		return m.fa
	}
	return m.en
}

var tokenWrongChat = message{
	en: "That doesn't belong in this chat — I've removed it. Open Sushi and tap the bot sign-in button; I'll meet you in the welcome chat.",
	fa: "این مال این چت نیست — پاکش کردم. سوشی رو باز کن و ورود با بات رو بزن؛ تو چت خوش‌آمد می‌بینمت.",
}
