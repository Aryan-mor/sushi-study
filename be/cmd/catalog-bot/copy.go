package main

import (
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
)

const (
	cbFilePrefix     = "c:f:"
	cbSeasonPrefix   = "c:s:"
	cbEpisodePrefix  = "c:e:"
	cbSeasonsPrefix  = "c:q:"
	cbRequestPrefix  = "c:r:"
	cbBulkPrefix     = "c:b:"
	cbBulkDLPrefix   = "c:d:"
	cbDownloadPrefix = "c:dl:"
	cbBackPrefix     = "c:bk:"
	cbHome           = "c:h"
	cbSearch         = "c:x"
	inlineShareMark  = ":share"
	episodesPerPage  = 30
	inlineQueryMax   = 256
)

const (
	homeTitle     = "📥 <b>دانلود</b>\n\nاسم فیلم یا سریال رو جستجو کن، یا از کانال خبر بازش کن."
	searchPrompt  = "🔍 اسم فیلم یا سریال رو بنویس — نتایج همین‌جا میان."
	searchBtn     = "🔍 جستجو"
	homeBtn       = "🏠 خانه"
	laterBtn      = "🔖 بعداً تماشا می‌کنم"
	playInAppBtn  = "▶ تماشای رایگان در اپ"
	shareBtn      = "📤 اشتراک‌گذاری"
	downloadBtn   = "📥 دانلود"
	requestBtn    = "📥 درخواست این عنوان"
	allEpsBtn     = "📥 دریافت همه قسمت‌ها"
	otherEpBtn    = "📺 دانلود اپیزود دیگر"
	backSeasons   = "⬅️ برگشت به فصل‌ها"
	backBtn       = "⬅️ بازگشت"
	prevEpBtn     = "◀️ قسمت قبل"
	nextEpBtn     = "▶️ قسمت بعد"
	pickDownload  = "برای دانلود روی دکمه زیر بزن 📥"
	pickQuality   = "کیفیت دلخواهت رو انتخاب کن ✨"
	pickSeason    = "فصل مدنظرت رو انتخاب کن 📺"
	pickEpisode   = "قسمت مدنظرت رو انتخاب کن ✨"
	pickBulk      = "کیفیت رو برای <b>دریافت همه قسمت‌ها</b> انتخاب کن ✨"
	shareFooter   = "می‌تونی فایل رو دانلود کنی یا با دوستات به اشتراک بذاری."
	missingTitle  = "این عنوان هنوز تو کتابخونه‌مون نیست — می‌تونی درخواستش کنی 📥"
	missingFiles  = "تو کتابخونه هست، ولی فایل آماده‌ای نداریم — می‌تونی درخواست بدی 🙏"
	busy          = "😅 یه مشکل موقتی پیش اومد. کمی بعد دوباره امتحان کن."
	sending       = "⏳ دارم می‌فرستم…"
	sendingBulk   = "⏳ دارم <b>%d</b> قسمت رو می‌فرستم… یه کم صبر کن ☕"
	noCopy        = "این فایل ارسال نشد. کیفیت دیگه رو امتحان کن."
	reqAccepted   = "درخواست ثبت شد. وقتی آماده بشه خبرت می‌کنیم."
	reqDuplicate  = "قبلاً همین رو خواستی."
	reqAvailable  = "همین حالا تو کتابخونه‌ست — کیفیت رو از بالا انتخاب کن."
	reqQuota      = "سهمیه درخواست امروزت تموم شد."
	ephemeralNote = "⚠️ این فایل تا 1 دقیقه دیگه از چت پاک می‌شه؛ لطفاً همین الان فورواردش کن 🙏"
	ephemeralBulk = "⚠️ فایل‌های ارسالی تا 1 دقیقه دیگه از چت پاک می‌شن؛ لطفاً همین الان فورواردشون کن 🙏"
	seasonFmt     = "فصل %d"
	epFmt         = "ق %d"
	kindMovie     = "فیلم"
	kindSeries    = "سریال"
)

func catalogCommands() []tg.BotCommand {
	return []tg.BotCommand{
		{Command: "start", Description: "خانه"},
		{Command: "search", Description: "جستجوی عنوان"},
	}
}
