package main

import "github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"

// Copy lives here per bot-chat locale until there is enough of it to deserve an i18n bundle
// (R-I18N-10). A missing translation falls back to English. The tone is the thing to preserve:
// warm, short, second person, and never telling somebody to type a command — actions are buttons
// (doc 13 §5). Bot-login token collection is the exception: Telegram ForceReply, same shape as
// the prototype's walkthrough, not a glass row.
//
// This locale is only how main-bot talks. It is not the app language.
type message struct {
	en, fa, ar, de, ru, uk, it, fr, tr string
}

func (m message) pick(locale string) string {
	var s string
	switch locale {
	case "fa":
		s = m.fa
	case "ar":
		s = m.ar
	case "de":
		s = m.de
	case "ru":
		s = m.ru
	case "uk":
		s = m.uk
	case "it":
		s = m.it
	case "fr":
		s = m.fr
	case "tr":
		s = m.tr
	default:
		s = m.en
	}
	if s == "" {
		return m.en
	}
	return s
}

// Fake BotFather token shape for the input placeholder only — never a real token (R-SEC).
const (
	botTokenPlaceholder  = "123456:ABC-defGhIjKlmNoPQRstuVWXyz"
	geminiKeyPlaceholder = "AIza… or AQ.…"
)

var (
	welcome = message{
		en: "Hey 👋 <b>Welcome to Sushi</b>\n\nOpen the app to sign in, or use the buttons below.",
		fa: "سلام 👋 <b>به سوشی خوش اومدی</b>\n\nبرای ورود اپ رو باز کن، یا از دکمه‌های زیر استفاده کن.",
		ar: "أهلاً 👋 <b>مرحباً بك في سوشي</b>\n\nافتح التطبيق لتسجيل الدخول، أو استخدم الأزرار بالأسفل.",
		de: "Hey 👋 <b>Willkommen bei Sushi</b>\n\nÖffne die App zum Anmelden, oder nutze die Buttons unten.",
		ru: "Привет 👋 <b>Добро пожаловать в Sushi</b>\n\nОткрой приложение, чтобы войти, или нажми кнопки ниже.",
		uk: "Привіт 👋 <b>Ласкаво просимо до Sushi</b>\n\nВідкрий застосунок, щоб увійти, або скористайся кнопками нижче.",
		it: "Ciao 👋 <b>Benvenuto su Sushi</b>\n\nApri l'app per accedere, oppure usa i pulsanti qui sotto.",
		fr: "Salut 👋 <b>Bienvenue sur Sushi</b>\n\nOuvre l'appli pour te connecter, ou utilise les boutons ci-dessous.",
		tr: "Selam 👋 <b>Sushi'ye hoş geldin</b>\n\nGiriş için uygulamayı aç, ya da aşağıdaki düğmeleri kullan.",
	}
	// Same text in every locale: they have not chosen yet, so the prompt itself must be readable
	// without a saved language. The choice applies only to this bot.
	askLanguage = message{
		en: languagePrompt,
		fa: languagePrompt,
	}
	// R-VOICE-1: onboarding confirmation still never names an API bot. Bot-login success is a
	// separate message (ADR 0013), not this string.
	assigned = message{
		en: "You're all set ✨\n\nOpen Sushi and start watching.",
		fa: "همه‌چی آماده‌ست ✨\n\nسوشی رو باز کن و شروع کن.",
		ar: "كل شيء جاهز ✨\n\nافتح سوشي وابدأ المشاهدة.",
		de: "Alles bereit ✨\n\nÖffne Sushi und leg los.",
		ru: "Всё готово ✨\n\nОткрой Sushi и начинай смотреть.",
		uk: "Усе готово ✨\n\nВідкрий Sushi і починай дивитися.",
		it: "Tutto pronto ✨\n\nApri Sushi e inizia a guardare.",
		fr: "Tout est prêt ✨\n\nOuvre Sushi et commence à regarder.",
		tr: "Her şey hazır ✨\n\nSushi'yi aç ve izlemeye başla.",
	}
	alreadySet = message{
		en: "You're already set up ✨ Open Sushi and pick up where you left off.",
		fa: "از قبل آماده‌ای ✨ سوشی رو باز کن و از همون‌جا ادامه بده.",
		ar: "حسابك جاهز من قبل ✨ افتح سوشي وتابع من حيث توقفت.",
		de: "Du bist schon eingerichtet ✨ Öffne Sushi und mach weiter.",
		ru: "Ты уже настроен ✨ Открой Sushi и продолжай с того места.",
		uk: "Ти вже налаштований ✨ Відкрий Sushi і продовжуй з того місця.",
		it: "Sei già a posto ✨ Apri Sushi e riprendi da dove eri.",
		fr: "Tu es déjà prêt ✨ Ouvre Sushi et reprends là où tu en étais.",
		tr: "Zaten hazırsın ✨ Sushi'yi aç ve kaldığın yerden devam et.",
	}
	failed = message{
		en: "😅 Something went wrong on my side. Give it a moment and tap again.",
		fa: "😅 یه مشکل از سمت من پیش اومد. یه لحظه صبر کن و دوباره بزن.",
		ar: "😅 صار خطأ من جهتي. انتظر لحظة واضغط مرة أخرى.",
		de: "😅 Bei mir ist etwas schiefgelaufen. Warte kurz und tippe nochmal.",
		ru: "😅 Что-то сломалось у меня. Подожди немного и нажми ещё раз.",
		uk: "😅 Щось пішло не так з мого боку. Зачекай хвилинку і натисни ще раз.",
		it: "😅 Qualcosa è andato storto da me. Aspetta un attimo e tocca di nuovo.",
		fr: "😅 Quelque chose a planté de mon côté. Attends un instant et réessaie.",
		tr: "😅 Bende bir sorun oldu. Biraz bekle ve tekrar bas.",
	}
	help = message{
		en: "I set your app up — pick a language, then I stay out of the way 🍣\n\n" +
			"Quality, audio, and taste picks live in the Sushi app after login.\n\n" +
			"Need the app itself? Tap Download on Home and pick your device.\n\n" +
			"Watching from the app without a personal Telegram account? Open Sushi, tap the salmon button, " +
			"and I'll walk you through BotFather. Reply here with the token — then come back to the app.\n\n" +
			"Need a new bot later? Tap Bot on Home. Want a person? Tap Support.",
		fa: "من اپت رو راه می‌ندازم — زبون رو می‌پرسم و بعد می‌رم کنار 🍣\n\n" +
			"کیفیت، صدا و سلیقه رو بعد لاگین تو خود اپ سوشی می‌پرسیم.\n\n" +
			"خود اپ؟ تو خانه دکمهٔ دانلود رو بزن و دستگاهت رو انتخاب کن.\n\n" +
			"از اپ بدون اکانت تلگرام؟ سوشی رو باز کن، دکمهٔ نارنجی رو بزن، با هم BotFather رو می‌ریم. " +
			"توکن رو همین‌جا ریپلای کن — بعد برگرد اپ.\n\n" +
			"بات جدید؟ تو خانه دکمهٔ بات رو بزن. با آدم حرف می‌زنی؟ پشتیبانی.",
		ar: "أجهّز تطبيقك — تختار لغة، ثم أبتعد 🍣\n\n" +
			"الجودة والصوت والذوق تكون داخل تطبيق سوشي بعد تسجيل الدخول.\n\n" +
			"تحتاج التطبيق؟ اضغط تنزيل في الرئيسية واختر جهازك.\n\n" +
			"تشاهد من التطبيق بدون حساب تيليغرام شخصي؟ افتح سوشي، اضغط الزر البرتقالي، " +
			"وأمشي معك عبر BotFather. أرسل التوكن هنا كرد — ثم ارجع إلى التطبيق.\n\n" +
			"بوت جديد لاحقاً؟ اضغط بوت في الرئيسية. تريد شخصاً؟ اضغط الدعم.",
		de: "Ich richte deine App ein — du wählst eine Sprache, dann halte ich mich raus 🍣\n\n" +
			"Qualität, Ton und Geschmack stellst du nach dem Login in der Sushi-App ein.\n\n" +
			"Die App selbst? Tippe auf Home auf Download und wähl dein Gerät.\n\n" +
			"Schauen ohne privates Telegram-Konto? Öffne Sushi, tippe den lachsfarbenen Button, " +
			"und ich führe dich durch BotFather. Antworte hier mit dem Token — dann zurück in die App.\n\n" +
			"Später einen neuen Bot? Tippe auf Home auf Bot. Eine Person? Tippe auf Support.",
		ru: "Я настраиваю приложение — выбери язык, и я отойду в сторону 🍣\n\n" +
			"Качество, звук и вкус настраиваются в приложении Sushi после входа.\n\n" +
			"Нужно само приложение? Нажми «Скачать» на главной и выбери устройство.\n\n" +
			"Смотришь из приложения без личного аккаунта Telegram? Открой Sushi, нажми оранжевую кнопку, " +
			"и я проведу тебя через BotFather. Ответь сюда токеном — потом вернись в приложение.\n\n" +
			"Новый бот позже? Нажми «Бот» на главной. Нужен человек? Нажми «Поддержка».",
		uk: "Я налаштовую застосунок — обери мову, і я відійду вбік 🍣\n\n" +
			"Якість, звук і смак налаштовуються в застосунку Sushi після входу.\n\n" +
			"Потрібен сам застосунок? Натисни «Завантажити» на головній і обери пристрій.\n\n" +
			"Дивишся із застосунку без особистого акаунта Telegram? Відкрий Sushi, натисни помаранчеву кнопку, " +
			"і я проведу тебе через BotFather. Відповідай сюди токеном — потім повернись у застосунок.\n\n" +
			"Новий бот пізніше? Натисни «Бот» на головній. Потрібна людина? Натисни «Підтримка».",
		it: "Preparo la tua app — scegli una lingua, poi mi faccio da parte 🍣\n\n" +
			"Qualità, audio e gusti stanno nell'app Sushi dopo l'accesso.\n\n" +
			"Ti serve l'app? Tocca Scarica nella Home e scegli il dispositivo.\n\n" +
			"Guardi dall'app senza un account Telegram personale? Apri Sushi, tocca il pulsante salmone, " +
			"e ti guido su BotFather. Rispondi qui con il token — poi torna all'app.\n\n" +
			"Un bot nuovo più tardi? Tocca Bot nella Home. Vuoi una persona? Tocca Supporto.",
		fr: "Je prépare ton appli — choisis une langue, puis je m'écarte 🍣\n\n" +
			"Qualité, audio et goûts se règlent dans l'appli Sushi après la connexion.\n\n" +
			"Il te faut l'appli ? Appuie sur Télécharger dans l'accueil et choisis ton appareil.\n\n" +
			"Tu regardes depuis l'appli sans compte Telegram personnel ? Ouvre Sushi, appuie sur le bouton saumon, " +
			"et je te guide dans BotFather. Réponds ici avec le jeton — puis reviens dans l'appli.\n\n" +
			"Un nouveau bot plus tard ? Appuie sur Bot dans l'accueil. Une personne ? Appuie sur Aide.",
		tr: "Uygulamayı ben kurarım — bir dil seç, sonra kenara çekilirim 🍣\n\n" +
			"Kalite, ses ve zevk, girişten sonra Sushi uygulamasında.\n\n" +
			"Uygulamanın kendisi mi? Ana sayfada İndir'e bas ve cihazını seç.\n\n" +
			"Kişisel bir Telegram hesabı olmadan uygulamadan mı izliyorsun? Sushi'yi aç, somon rengi düğmeye bas, " +
			"BotFather'da birlikte gideriz. Token'ı buraya yanıt olarak gönder — sonra uygulamaya dön.\n\n" +
			"Sonra yeni bir bot mu? Ana sayfada Bot'a bas. Bir insan mı? Destek.",
	}
	unknown = message{
		en: "I only set your app up 🍣 Open Sushi, or use the buttons below.",
		fa: "من فقط اپت رو راه می‌ندازم 🍣 سوشی رو باز کن، یا از دکمه‌های زیر استفاده کن.",
		ar: "أنا فقط أجهّز تطبيقك 🍣 افتح سوشي، أو استخدم الأزرار بالأسفل.",
		de: "Ich richte nur deine App ein 🍣 Öffne Sushi, oder nutze die Buttons unten.",
		ru: "Я только настраиваю приложение 🍣 Открой Sushi или нажми кнопки ниже.",
		uk: "Я лише налаштовую застосунок 🍣 Відкрий Sushi або скористайся кнопками нижче.",
		it: "Preparo solo la tua app 🍣 Apri Sushi, oppure usa i pulsanti qui sotto.",
		fr: "Je prépare seulement ton appli 🍣 Ouvre Sushi, ou utilise les boutons ci-dessous.",
		tr: "Ben sadece uygulamayı kurarım 🍣 Sushi'yi aç, ya da aşağıdaki düğmeleri kullan.",
	}
	botSetupIntro = message{
		en: "🤖 <b>One more step — connect a bot you own</b>\n\n" +
			"Sushi streams through a small bot of yours, not your personal Telegram account.\n\n" +
			"<b>1.</b> Open @BotFather and tap <b>Open</b> at the bottom (the <b>Mini App</b>).\n" +
			"<b>2.</b> Create the bot there: pick a name and username. Copy the <b>token</b> — a long string like <code>123456:ABC-def...</code>.\n" +
			"<b>3.</b> Stay in that Mini App. Pick the bot → <b>Bot Settings</b> → turn on <b>Bot to Bot Communication Mode</b>. Not a chat command.\n" +
			"<b>4.</b> Send that token here, as a <b>reply</b> to this message.\n\n" +
			"This bot is yours — I'll finish setup and you go back to the app.",
		fa: "🤖 <b>یک مرحله دیگه مونده — بات خودت رو وصل کن</b>\n\n" +
			"سوشی با یک بات کوچیک از خودت استریم می‌کنه، نه با اکانت تلگرامت.\n\n" +
			"<b>1.</b> به @BotFather برو و پایین صفحه دکمهٔ <b>Open</b> رو بزن (<b>مینی‌اپ</b>).\n" +
			"<b>2.</b> همون‌جا بات رو بساز: اسم و username انتخاب کن. <b>توکن</b> رو کپی کن — رشته‌ای شبیه <code>123456:ABC-def...</code>.\n" +
			"<b>3.</b> تو همون مینی‌اپ بمون. باتت رو انتخاب کن ← <b>Bot Settings</b> ← <b>Bot to Bot Communication Mode</b> رو روشن کن. با دستور چت نیست.\n" +
			"<b>4.</b> اون توکن رو اینجا، به‌عنوان <b>ریپلای</b> به همین پیام بفرست.\n\n" +
			"این بات مال خودته — تموم که شد برگرد اپ.",
	}
	appCodeLead = message{
		en: "Hooray ✅ you're in!\n\nGo back to Sushi — it will finish signing you in.",
		fa: "هورااا ✅ لاگین شد!\n\nبرگرد سوشی — خودش ورود رو تموم می‌کنه.",
	}
	appCodeNeedApp = message{
		en: "Almost — open Sushi and tap the salmon Login button, then come back here.",
		fa: "نزدیکه — سوشی رو باز کن، دکمهٔ نارنجی ورود رو بزن، بعد برگرد اینجا.",
	}
	enrollBadToken = message{
		en: "That token doesn't work. Check it in BotFather and <b>reply</b> with a fresh one.",
		fa: "این توکن کار نمی‌کنه. تو BotFather چک کن و یکی تازه رو <b>ریپلای</b> کن.",
	}
	enrollNeedBotToBot = message{
		en: "Almost there — your bot isn't allowed to message other bots yet.\n\n" +
			"Open @BotFather and launch its <b>Mini App</b> (or tap <b>Open</b> at the bottom). Pick your bot → <b>Bot Settings</b> → " +
			"turn on <b>Bot to Bot Communication Mode</b>. That switch is in the Mini App, not a chat command. " +
			"Then <b>reply</b> here with the same token.",
		fa: "کارِت تقریباً تمومه — بات تو هنوز اجازه نداره به بات‌های دیگه پیام بده.\n\n" +
			"@BotFather رو باز کن و <b>مینی‌اپ</b>ش رو اجرا کن (یا پایین صفحه دکمهٔ <b>Open</b> رو بزن). باتت رو انتخاب کن ← <b>Bot Settings</b> ← " +
			"<b>Bot to Bot Communication Mode</b> رو روشن کن. این گزینه تو مینی‌اپ است، نه با دستور چت. " +
			"بعد همون توکن رو اینجا <b>ریپلای</b> کن.",
	}
	enrollFailed = message{
		en: "😅 Something went wrong on my side. Reply with the token again in a moment.",
		fa: "😅 یه مشکل از سمت من پیش اومد. یه لحظه بعد توکن رو دوباره ریپلای کن.",
	}
	reconnectIntro = message{
		en: "🤖 <b>New bot?</b>\n\n" +
			"Revoke the old one in @BotFather if you still can, then same walkthrough:\n\n" +
			"<b>1.</b> @BotFather → tap <b>Open</b> (the <b>Mini App</b>). Create a bot there, or pick the one you already have.\n" +
			"<b>2.</b> Copy the <b>token</b>.\n" +
			"<b>3.</b> Stay in that Mini App → <b>Bot Settings</b> → turn on <b>Bot to Bot Communication Mode</b>.\n" +
			"<b>4.</b> <b>Reply</b> here with the token.\n\n" +
			"I'll swap it in. Then open Sushi and tap Login.",
		fa: "🤖 <b>بات جدید؟</b>\n\n" +
			"اگه هنوز می‌تونی، قبلی رو تو @BotFather باطل کن، بعد همون مراحل:\n\n" +
			"<b>1.</b> @BotFather ← دکمهٔ <b>Open</b> (<b>مینی‌اپ</b>). همون‌جا بات بساز، یا همون بات قبلی رو انتخاب کن.\n" +
			"<b>2.</b> <b>توکن</b> رو کپی کن.\n" +
			"<b>3.</b> تو همون مینی‌اپ ← <b>Bot Settings</b> ← <b>Bot to Bot Communication Mode</b> رو روشن کن.\n" +
			"<b>4.</b> توکن رو اینجا <b>ریپلای</b> کن.\n\n" +
			"جاش می‌ذارم. بعد سوشی رو باز کن و ورود رو بزن.",
	}
	aiKeyIntro = message{
		en: "🔑 <b>Gemini API key</b>\n\n" +
			"Free key: open <a href=\"https://aistudio.google.com/apikey\">aistudio.google.com/apikey</a> → " +
			"<b>Create API key</b> → Google sign-in, no payment → paste it here as a <b>reply</b>.\n\n" +
			"Used only to translate subtitles to Persian. Never shipped in the app.",
		fa: "🔑 <b>کلید Gemini</b>\n\n" +
			"کلید رایگان: برو <a href=\"https://aistudio.google.com/apikey\">aistudio.google.com/apikey</a> ← " +
			"<b>Create API key</b> ← ورود گوگل، بدون پرداخت ← همین‌جا به‌عنوان <b>ریپلای</b> پیست کن.\n\n" +
			"فقط برای ترجمهٔ زیرنویس به فارسی. تو اپ ذخیره نمی‌شه.",
	}
	aiKeySaved = message{
		en: "✅ Gemini key saved. Open Sushi and tap Translate again.",
		fa: "✅ کلید Gemini ذخیره شد. سوشی رو باز کن و دوباره ترجمه رو بزن.",
	}
	aiKeyBad = message{
		en: "That doesn't look like a Gemini key. It should start with <code>AIza</code> or <code>AQ.</code>. " +
			"Copy it from aistudio.google.com/apikey and <b>reply</b> here.",
		fa: "این شبیه کلید Gemini نیست. باید با <code>AIza</code> یا <code>AQ.</code> شروع بشه. " +
			"از aistudio.google.com/apikey کپی کن و اینجا <b>ریپلای</b> کن.",
	}
	aiKeyRemoveAsk = message{
		en: "Remove the saved Gemini key? Translate to Persian will stop until you add another.",
		fa: "کلید Gemini حذف بشه؟ تا وقتی یکی جدید نزاری، ترجمه به فارسی کار نمی‌کنه.",
	}
	aiKeyRemoved = message{
		en: "🧹 Gemini key removed.",
		fa: "🧹 کلید Gemini حذف شد.",
	}
	aiKeyStoreFailed = message{
		en: "😅 Couldn't save the key on my side. Reply with it again in a moment.",
		fa: "😅 نتونستم کلید رو ذخیره کنم. یه لحظه بعد دوباره ریپلای کن.",
	}
	aiKeyBtnRemove    = message{en: "🗑 Remove key", fa: "🗑 حذف کلید"}
	aiKeyBtnRemoveYes = message{en: "🗑 Yes, remove", fa: "🗑 بله، حذف کن"}
)

const (
	cbLocalePrefix = "l:"
	cbMenuLogin    = "m:login"
	cbMenuLanguage = "m:lang"
	cbMenuHelp     = "m:help"
	cbMenuHome     = "m:home"
	cbMenuApp      = "m:app"
	cbMenuBot      = "m:bot"
	cbMenuSettings = "m:set"
	// Legacy settings callbacks — never re-add to keyboards; quality/audio live in the app wizard.
	cbSetQualityOpen = "m:setq"
	cbSetAudioOpen   = "m:seta"
	cbSetAiKey       = "m:aik"
	cbAiKeyReplace   = "m:aikrep"
	cbAiKeyCancel    = "m:aikcan"
	cbAiKeyRemove    = "m:aikrm"
	cbAiKeyRemoveYes = "m:aikry"
	cbMenuSupport    = "m:sup"
	cbMenuDelete     = "m:del"
	cbDelNext        = "da:n"
	cbDelYes         = "da:y"
	cbDelNo          = "da:x"
	cbDelRevive      = "da:go"
	cbBotTest        = "bt:test"
	cbBotReplace     = "bt:rep"
	cbBotCancel      = "bt:can"
	cbBotRemove      = "bt:rm"
	cbBotRemoveYes   = "bt:rmy"
	// Staff hub (/admin). Nested panels keep their own prefixes; callback_data is 64 bytes.
	cbMenuAdmin             = "m:adm"
	cbMenuRequests          = "m:req"
	cbReqPagePrefix         = "rq:p:"
	cbReqRejectPrefix       = "rq:x:"
	cbReqRecheckPrefix      = "rq:c:"
	cbReqAlertRejectPrefix  = "rq:ax:"
	cbReqAlertRecheckPrefix = "rq:ac:"
	cbMenuActions           = "m:act"
	cbMenuIDs               = "m:ids"
	cbIDsCancel             = "m:idc"
	cbMenuNews              = "m:nws"
	cbNewsDraftCancel       = "m:nwc"
	cbIxOn                  = "ix:on"
	cbIxOff                 = "ix:off"
	cbIxROn                 = "ix:ron"
	cbIxROff                = "ix:roff"
	cbIxBatchUp             = "ix:b+"
	cbIxBatchDn             = "ix:b-"
	cbIxPassUp              = "ix:p+"
	cbIxPassDn              = "ix:p-"
	cbIxPaceUp              = "ix:c+"
	cbIxPaceDn              = "ix:c-"
	cbIxSources             = "ix:src"
	cbIxAddPriority         = "ix:ah"
	cbIxAddPrimary          = "ix:ap"
	cbIxAddSecondary        = "ix:as"
	cbIxAddSubtitle         = "ix:asub"
	cbIxAddCancel           = "ix:can"
	cbIxViewPrefix          = "ix:v:"
	cbIxTogglePrefix        = "ix:t:"
	cbIxPriorityPrefix      = "ix:0:"
	cbIxPrimaryPrefix       = "ix:1:"
	cbIxSecondPrefix        = "ix:2:"
	cbIxRemovePrefix        = "ix:x:"
	cbIxRemoveYesPrefix     = "ix:y:"
	// The announce channel (ADR 0017). Approve / reject ride on a staff preview DM; the watch-later
	// prefix rides on the published channel post, where the presser is any reader, not staff.
	cbNewsApprovePrefix = "nw:a:"
	cbNewsRejectPrefix  = "nw:x:"
	cbNewsLaterPrefix   = "nw:l:"
	cbNewsKindPrefix    = "nw:k:"
)

func homeKeyboard(locale string) tg.InlineKeyboard {
	return homeKeyboardFor(locale, "")
}

// homeKeyboardFor is the /start screen. Download leads, alone and tinted primary, because it is the
// one thing a new arrival is here for; everything else pairs up two to a row so Home reads as a
// short grid instead of a column of six. Requests, Index, Indexer, TMDB↔IMDb, and Backup live inside the /admin hub, not on Home
// (the prototype-shaped). Login is the app deep link, not a glass button. Language lives under
// Settings; quality / audio / taste live in the app wizard.
func homeKeyboardFor(locale, role string) tg.InlineKeyboard {
	helpBtn := message{
		en: "📖 Help", fa: "📖 راهنما", ar: "📖 مساعدة", de: "📖 Hilfe",
		ru: "📖 Помощь", uk: "📖 Довідка", it: "📖 Aiuto", fr: "📖 Aide", tr: "📖 Yardım",
	}
	settingsBtn := message{
		en: "⚙️ Settings", fa: "⚙️ تنظیمات", ar: "⚙️ الإعدادات", de: "⚙️ Einstellungen",
		ru: "⚙️ Настройки", uk: "⚙️ Налаштування", it: "⚙️ Impostazioni", fr: "⚙️ Réglages", tr: "⚙️ Ayarlar",
	}
	aiKeyBtn := message{en: "🔑 Gemini", fa: "🔑 Gemini"}
	botBtn := message{
		en: "🤖 Bot", fa: "🤖 بات", ar: "🤖 بوت", de: "🤖 Bot",
		ru: "🤖 Бот", uk: "🤖 Бот", it: "🤖 Bot", fr: "🤖 Bot", tr: "🤖 Bot",
	}
	supportBtn := message{
		en: "💬 Support", fa: "💬 پشتیبانی", ar: "💬 الدعم", de: "💬 Support",
		ru: "💬 Поддержка", uk: "💬 Підтримка", it: "💬 Supporto", fr: "💬 Aide", tr: "💬 Destek",
	}
	rows := [][]tg.InlineButton{
		{{Text: dlBtn.pick(locale), Data: cbMenuDownload, Style: tg.StylePrimary}},
		{
			{Text: settingsBtn.pick(locale), Data: cbMenuSettings},
			{Text: helpBtn.pick(locale), Data: cbMenuHelp},
		},
		{
			{Text: aiKeyBtn.pick(locale), Data: cbSetAiKey},
			{Text: botBtn.pick(locale), Data: cbMenuBot},
		},
	}
	// Support shares the last row with the staff hub when there is one, so adding Admin does not
	// cost a row.
	last := []tg.InlineButton{{Text: supportBtn.pick(locale), Data: cbMenuSupport}}
	if isAdmin(role) {
		last = append(last, tg.InlineButton{Text: adminBtn.pick(locale), Data: cbMenuAdmin})
	}
	return tg.InlineKeyboard{Rows: append(rows, last)}
}

var (
	adminPanelTitle = message{en: "🛠 <b>Admin</b>\n\nChoose an action.", fa: "🛠 <b>مدیریت</b>\n\nیک کار را انتخاب کنید."}
	adminBtn        = message{en: "🛠 Admin", fa: "🛠 مدیریت"}
	adminBtnBack    = message{en: "⬅️ Admin", fa: "⬅️ مدیریت"}
	actionsBtn      = message{en: "🧰 Actions", fa: "🧰 اکشن‌ها"}
	actionsTitle    = message{en: "🧰 <b>Actions</b>\n\nOps tools. Who is online, daily stats, or a backup of both databases and Infisical.", fa: "🧰 <b>اکشن‌ها</b>\n\nابزار عملیاتی. افراد آنلاین، آمار روزانه، یا بکاپ هر دو دیتابیس و Infisical."}
	actionsBtnBack  = message{en: "⬅️ Actions", fa: "⬅️ اکشن‌ها"}
	reqPanelBtn     = message{en: "📥 Requests", fa: "📥 درخواست‌ها"}
	reqPanelTitle   = message{en: "📥 <b>Content requests</b>", fa: "📥 <b>درخواست‌های محتوا</b>"}
	reqPanelEmpty   = message{en: "No open requests.", fa: "درخواست بازی نیست."}
	reqPanelFailed  = message{en: "😅 Couldn't load the requests. Try again.", fa: "😅 لود نشد. دوباره امتحان کن."}
	reqBtnReject    = message{en: "❌ Reject", fa: "❌ رد"}
	reqBtnRecheck   = message{en: "🔄 Re-check", fa: "🔄 بررسی دوباره"}
	reqBtnNextPage  = message{en: "▶️ Next 10", fa: "▶️ 10 تای بعد"}
	reqBtnHome      = message{
		en: "🏠 Home", fa: "🏠 خانه", ar: "🏠 الرئيسية", de: "🏠 Home",
		ru: "🏠 Главная", uk: "🏠 Головна", it: "🏠 Home", fr: "🏠 Accueil", tr: "🏠 Ana sayfa",
	}
	reqToastReject  = message{en: "Rejected.", fa: "رد شد."}
	reqToastAdded   = message{en: "Added ✅ — the requester will be notified.", fa: "اضافه شد ✅ — به درخواست‌دهنده خبر می‌ره."}
	reqToastPending = message{en: "Not in the catalog yet.", fa: "هنوز تو کاتالوگ نیست."}
	reqToastGone    = message{en: "That request is no longer open.", fa: "این درخواست دیگه باز نیست."}
	reqToastDenied  = message{en: "Admins only.", fa: "فقط ادمین."}
	// Auto-fetch toasts (ADR 0035 D1). %d is the number of films/episodes queued, %d new.
	superToastDenied = message{en: "Superadmins only.", fa: "فقط سوپرادمین."}
)

// The announce channel (ADR 0017). The post itself is Persian and lives in app/newsroom; everything
// here is either a staff-facing preview control or a toast to one person, so both locales apply.
var (
	newsBtnApprove = message{en: "✅ Post it", fa: "✅ منتشر کن"}
	newsBtnReject  = message{en: "🚫 Skip", fa: "🚫 بی‌خیال"}
	// Channel-post labels are Persian only: the audience reads the channel in one language, and
	// a URL keyboard cannot vary per reader (ADR 0017, ADR 0022).
	newsBtnApp      = "🎬 تماشا رایگان در اپ"
	newsBtnLater    = "🔖 بعداً تماشا"
	newsBtnDownload = "📥 دانلود"

	newsAppDl = message{
		en: "Have Sushi? Open it — this title is in Watch Later, first on the slider.\nNo app yet? Pick your device below.",
		fa: "سوشی نصب هست؟ برو تو اپ — این عنوان می‌ره تو «بعداً تماشا» و اول اسلایدر میاد.\nنصب نیست؟ دستگاهت رو بزن، فایل همین‌جاست.",
	}

	newsToastPosted    = message{en: "Posted to the channel.", fa: "در کانال منتشر شد."}
	newsToastRejected  = message{en: "Skipped — it will not be posted.", fa: "رد شد — منتشر نمی‌شه."}
	newsToastHandled   = message{en: "Already handled.", fa: "قبلاً رسیدگی شده."}
	newsToastFailed    = message{en: "😅 That didn't go through. Try again.", fa: "😅 نشد. دوباره امتحان کن."}
	newsToastNoChannel = message{
		en: "No announce channel is configured.",
		fa: "کانال اعلام تنظیم نشده.",
	}
	newsToastDenied = message{en: "Admins only.", fa: "فقط ادمین."}

	// The watch-later toasts answer a press in the channel, so they are written for a reader, not
	// for staff — and never mention where the flag is stored (R-VOICE-1).
	newsToastLaterAdded = message{
		en: "Saved to Watch Later ✨",
		fa: "ذخیره شد ✨ بعداً در سوشی پیداش می‌کنی",
	}
	newsToastLaterNeedsAccount = message{
		en: "Open Sushi first, then tap this again.",
		fa: "اول سوشی رو باز کن، بعد دوباره بزن.",
	}
	newsToastLaterBusy = message{
		en: "Too many taps — give it a minute.",
		fa: "زیاد زدی — یه دقیقه صبر کن.",
	}
)

// languagePrompt is shown before a locale exists, so every line is in its own language.
// The choice is only how this bot writes back — not the Sushi app language.
const languagePrompt = "☀️🦁 <b>زبان این ربات را انتخاب کن</b>\n" +
	"☀️🦁 این انتخاب فقط برای حرف زدن با همین ربات است.\n\n" +
	"🇬🇧 <b>Choose this bot's language</b>\n" +
	"This choice is only for chatting with this bot.\n\n" +
	"🇸🇦 <b>اختر لغة هذا البوت</b>\n" +
	"هذا الاختيار فقط للحديث مع هذا البوت.\n\n" +
	"🇩🇪 <b>Wähle die Sprache dieses Bots</b>\n" +
	"Die Wahl gilt nur für diesen Bot.\n\n" +
	"🇷🇺 <b>Выбери язык этого бота</b>\n" +
	"Выбор только для общения с этим ботом.\n\n" +
	"🇺🇦 <b>Обери мову цього бота</b>\n" +
	"Вибір лише для спілкування з цим ботом.\n\n" +
	"🇮🇹 <b>Scegli la lingua di questo bot</b>\n" +
	"La scelta vale solo per questo bot.\n\n" +
	"🇫🇷 <b>Choisis la langue de ce bot</b>\n" +
	"Ce choix ne concerne que ce bot.\n\n" +
	"🇹🇷 <b>Bu botun dilini seç</b>\n" +
	"Bu seçim sadece bu botla konuşmak içindir."

func languageKeyboard() tg.InlineKeyboard {
	type item struct{ code, label string }
	items := []item{
		{"fa", "☀️🦁 فارسی"},
		{"en", "🇬🇧 English"},
		{"ar", "🇸🇦 العربية"},
		{"de", "🇩🇪 Deutsch"},
		{"ru", "🇷🇺 Русский"},
		{"uk", "🇺🇦 Українська"},
		{"it", "🇮🇹 Italiano"},
		{"fr", "🇫🇷 Français"},
		{"tr", "🇹🇷 Türkçe"},
	}
	rows := make([][]tg.InlineButton, 0, len(items)/2)
	var row []tg.InlineButton
	for _, it := range items {
		row = append(row, tg.InlineButton{Text: it.label, Data: cbLocalePrefix + it.code})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tg.InlineKeyboard{Rows: rows}
}

func menuCommands(locale string) []tg.BotCommand {
	return menuCommandsFor(locale, "")
}

func menuCommandsFor(locale, role string) []tg.BotCommand {
	start := message{
		en: "Welcome", fa: "خوش‌آمد", ar: "ترحيب", de: "Willkommen",
		ru: "Приветствие", uk: "Вітання", it: "Benvenuto", fr: "Accueil", tr: "Hoş geldin",
	}
	login := message{
		en: "Log in to Sushi", fa: "ورود به سوشی", ar: "الدخول إلى سوشي", de: "Bei Sushi anmelden",
		ru: "Войти в Sushi", uk: "Увійти в Sushi", it: "Accedi a Sushi", fr: "Connexion à Sushi", tr: "Sushi'ye giriş",
	}
	helpDesc := message{
		en: "What I can help with", fa: "چطور کمکت کنم", ar: "بماذا أساعد", de: "Wobei ich helfe",
		ru: "Чем я могу помочь", uk: "Чим я можу допомогти", it: "Come posso aiutare", fr: "Ce que je peux faire", tr: "Nasıl yardımcı olurum",
	}
	settings := message{
		en: "Settings", fa: "تنظیمات", ar: "الإعدادات", de: "Einstellungen",
		ru: "Настройки", uk: "Налаштування", it: "Impostazioni", fr: "Réglages", tr: "Ayarlar",
	}
	language := message{
		en: "Change language", fa: "تغییر زبان", ar: "تغيير اللغة", de: "Sprache ändern",
		ru: "Сменить язык", uk: "Змінити мову", it: "Cambia lingua", fr: "Changer de langue", tr: "Dili değiştir",
	}
	download := message{
		en: "Download the app", fa: "دانلود اپ", ar: "تنزيل التطبيق", de: "App herunterladen",
		ru: "Скачать приложение", uk: "Завантажити застосунок", it: "Scarica l'app", fr: "Télécharger l'appli", tr: "Uygulamayı indir",
	}
	cmds := []tg.BotCommand{
		{Command: "start", Description: start.pick(locale)},
		{Command: "download", Description: download.pick(locale)},
		{Command: "login", Description: login.pick(locale)},
		{Command: "help", Description: helpDesc.pick(locale)},
		{Command: "settings", Description: settings.pick(locale)},
		{Command: "language", Description: language.pick(locale)},
	}
	if isAdmin(role) {
		admin := message{en: "Admin", fa: "مدیریت"}
		cmds = append(cmds, tg.BotCommand{Command: "admin", Description: admin.pick(locale)})
	}
	return cmds
}
