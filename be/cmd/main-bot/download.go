package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

const (
	cbMenuDownload = "m:dl"
	cbDlPrefix     = "dl:"

	platAndroidNew = "android_new"
	platAndroidOld = "android_old"
	platAndroidTV  = "android_tv"
	platWindows    = "windows"
	platLinux      = "linux"
	platMacOS      = "macos"
	platIOS        = "ios"
)

type appReleaseStore interface {
	UpsertAppRelease(
		ctx context.Context, platform, track string, channelID, messageID int64, version, fileName string,
	) (prevMessageID int64, err error)
	AppRelease(ctx context.Context, platform string) (channelID, messageID int64, ok bool, err error)
	AppReleasePlatforms(ctx context.Context) (map[string]string, error)
}

var (
	dlBtn = message{
		en: "⬇️ Download", fa: "⬇️ دانلود", ar: "⬇️ تنزيل", de: "⬇️ Download",
		ru: "⬇️ Скачать", uk: "⬇️ Завантажити", it: "⬇️ Scarica", fr: "⬇️ Télécharger", tr: "⬇️ İndir",
	}
	dlTitle = message{
		en: "⬇️ <b>Download Sushi</b>\n\nPick your device. I'll send the latest file here.",
		fa: "⬇️ <b>دانلود سوشی</b>\n\nدستگاهت رو بزن. آخرین فایل رو همین‌جا می‌فرستم.",
		ar: "⬇️ <b>تنزيل سوشي</b>\n\nاختر جهازك. أرسل أحدث ملف هنا.",
		de: "⬇️ <b>Sushi herunterladen</b>\n\nWähl dein Gerät. Ich schicke die neueste Datei hierher.",
		ru: "⬇️ <b>Скачать Sushi</b>\n\nВыбери устройство. Пришлю сюда последний файл.",
		uk: "⬇️ <b>Завантажити Sushi</b>\n\nОбери пристрій. Надішлю сюди останній файл.",
		it: "⬇️ <b>Scarica Sushi</b>\n\nScegli il dispositivo. Mando qui l'ultimo file.",
		fr: "⬇️ <b>Télécharger Sushi</b>\n\nChoisis ton appareil. J'envoie le dernier fichier ici.",
		tr: "⬇️ <b>Sushi'yi indir</b>\n\nCihazını seç. En son dosyayı buraya gönderirim.",
	}
	dlBtnNewAndroid = message{
		en: "📱 New Android", fa: "📱 اندروید جدید", ar: "📱 أندرويد جديد", de: "📱 Neues Android",
		ru: "📱 Новый Android", uk: "📱 Новий Android", it: "📱 Android nuovo", fr: "📱 Android récent", tr: "📱 Yeni Android",
	}
	dlBtnOldAndroid = message{
		en: "📱 Old Android", fa: "📱 اندروید قدیمی", ar: "📱 أندرويد قديم", de: "📱 Altes Android",
		ru: "📱 Старый Android", uk: "📱 Старий Android", it: "📱 Android vecchio", fr: "📱 Android ancien", tr: "📱 Eski Android",
	}
	dlBtnAndroidTV = message{
		en: "📺 Android TV", fa: "📺 اندروید تی‌وی", ar: "📺 Android TV", de: "📺 Android TV",
		ru: "📺 Android TV", uk: "📺 Android TV", it: "📺 Android TV", fr: "📺 Android TV", tr: "📺 Android TV",
	}
	dlBtnWindows = message{
		en: "💻 Windows", fa: "💻 ویندوز", ar: "💻 ويندوز", de: "💻 Windows",
		ru: "💻 Windows", uk: "💻 Windows", it: "💻 Windows", fr: "💻 Windows", tr: "💻 Windows",
	}
	dlBtnLinux = message{
		en: "🐧 Linux", fa: "🐧 لینوکس", ar: "🐧 لينكس", de: "🐧 Linux",
		ru: "🐧 Linux", uk: "🐧 Linux", it: "🐧 Linux", fr: "🐧 Linux", tr: "🐧 Linux",
	}
	dlBtnMacOS = message{
		en: "🍎 macOS", fa: "🍎 مک", ar: "🍎 ماك", de: "🍎 macOS",
		ru: "🍎 macOS", uk: "🍎 macOS", it: "🍎 macOS", fr: "🍎 macOS", tr: "🍎 macOS",
	}
	dlBtnIOS = message{
		en: "📱 iOS", fa: "📱 آیفون", ar: "📱 iOS", de: "📱 iOS",
		ru: "📱 iOS", uk: "📱 iOS", it: "📱 iOS", fr: "📱 iOS", tr: "📱 iOS",
	}
	dlToastSend = message{
		en: "Sending…", fa: "داره میاد…", ar: "جارٍ الإرسال…", de: "Sende…",
		ru: "Отправляю…", uk: "Надсилаю…", it: "Invio…", fr: "Envoi…", tr: "Gönderiliyor…",
	}
	dlToastMissing = message{
		en: "That build isn't up yet. Try another device.",
		fa: "این نسخه هنوز نیومده. یکی دیگه رو بزن.",
		ar: "هذا الإصدار غير متوفر بعد. جرّب جهازاً آخر.",
		de: "Dieser Build ist noch nicht da. Versuch ein anderes Gerät.",
		ru: "Этой сборки ещё нет. Попробуй другое устройство.",
		uk: "Цієї збірки ще немає. Спробуй інший пристрій.",
		it: "Questa build non c'è ancora. Prova un altro dispositivo.",
		fr: "Cette version n'est pas encore là. Essaie un autre appareil.",
		tr: "Bu sürüm henüz yok. Başka bir cihaz dene.",
	}
	dlToastSoonIOS = message{
		en: "iOS is coming soon.",
		fa: "نسخه آیفون به زودی میاد.",
		ar: "iOS قريباً.",
		de: "iOS kommt bald.",
		ru: "iOS скоро будет.",
		uk: "iOS незабаром.",
		it: "iOS arriva presto.",
		fr: "iOS arrive bientôt.",
		tr: "iOS yakında.",
	}
	dlToastSoonMac = message{
		en: "macOS is coming soon.",
		fa: "نسخه مک به زودی میاد.",
		ar: "ماك قريباً.",
		de: "macOS kommt bald.",
		ru: "macOS скоро будет.",
		uk: "macOS незабаром.",
		it: "macOS arriva presto.",
		fr: "macOS arrive bientôt.",
		tr: "macOS yakında.",
	}
	dlToastNoChannel = message{
		en: "Downloads aren't ready yet. Try again later.",
		fa: "دانلود هنوز آماده نیست. یه کم دیگه بیا.",
		ar: "التنزيل غير جاهز بعد. حاول لاحقاً.",
		de: "Downloads sind noch nicht bereit. Versuch es später.",
		ru: "Скачивание ещё не готово. Зайди позже.",
		uk: "Завантаження ще не готове. Спробуй пізніше.",
		it: "I download non sono ancora pronti. Riprova più tardi.",
		fr: "Les téléchargements ne sont pas encore prêts. Réessaie plus tard.",
		tr: "İndirmeler henüz hazır değil. Sonra tekrar dene.",
	}
	dlSent = message{
		en: "Here you go ✨", fa: "بفرما ✨", ar: "تفضل ✨", de: "Bitte schön ✨",
		ru: "Держи ✨", uk: "Тримай ✨", it: "Ecco a te ✨", fr: "Voilà ✨", tr: "Buyur ✨",
	}
	dlEmpty = message{
		en: "⬇️ <b>Download Sushi</b>\n\nNo build is up yet. I'll have one here as soon as it's released.",
		fa: "⬇️ <b>دانلود سوشی</b>\n\nهنوز نسخه‌ای نیومده. تا منتشر بشه همین‌جا می‌ذارم.",
		ar: "⬇️ <b>تنزيل سوشي</b>\n\nلا يوجد إصدار بعد. أضعه هنا فور صدوره.",
		de: "⬇️ <b>Sushi herunterladen</b>\n\nNoch kein Build. Sobald einer raus ist, liegt er hier.",
		ru: "⬇️ <b>Скачать Sushi</b>\n\nСборки ещё нет. Как выйдет — положу сюда.",
		uk: "⬇️ <b>Завантажити Sushi</b>\n\nЗбірки ще немає. Щойно вийде — покладу сюди.",
		it: "⬇️ <b>Scarica Sushi</b>\n\nNessuna build ancora. Appena esce, la metto qui.",
		fr: "⬇️ <b>Télécharger Sushi</b>\n\nPas encore de version. Dès qu'elle sort, elle sera ici.",
		tr: "⬇️ <b>Sushi'yi indir</b>\n\nHenüz bir sürüm yok. Çıkar çıkmaz buraya koyarım.",
	}
)

// dlPlatformOrder is the order devices are offered in, most-asked-for first. It is also the only
// list of platforms the picker knows: a platform missing from `app_releases` is left out entirely
// rather than shown as a button that answers "not up yet" (the screen must not promise a file it
// cannot send). macOS and iOS are the exception: the button stays, the tap says it is coming soon,
// and a superadmin hears which user asked. Both need a Mac runner and there is none (ADR 0016).
var dlComingSoon = map[string]struct {
	toast message
	label string
}{
	platMacOS: {dlToastSoonMac, "🍎 macOS"},
	platIOS:   {dlToastSoonIOS, "📱 iOS"},
}

func isComingSoon(platform string) bool {
	_, ok := dlComingSoon[platform]
	return ok
}

var dlPlatformOrder = []struct {
	platform string
	btn      message
}{
	{platAndroidNew, dlBtnNewAndroid},
	{platAndroidOld, dlBtnOldAndroid},
	{platAndroidTV, dlBtnAndroidTV},
	{platWindows, dlBtnWindows},
	{platLinux, dlBtnLinux},
	{platMacOS, dlBtnMacOS},
	{platIOS, dlBtnIOS},
}

// downloadKeyboard lays the available platforms out two to a row, Home last. available is
// platform → version, as stored by the channel-post handler.
func downloadKeyboard(locale string, available map[string]string) tg.InlineKeyboard {
	var rows [][]tg.InlineButton
	var row []tg.InlineButton
	for _, p := range dlPlatformOrder {
		if _, ok := available[p.platform]; !ok && !isComingSoon(p.platform) {
			continue
		}
		row = append(row, tg.InlineButton{Text: p.btn.pick(locale), Data: cbDlPrefix + p.platform})
		if len(row) == 2 {
			rows, row = append(rows, row), nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tg.InlineKeyboard{Rows: append(rows, []tg.InlineButton{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}})}
}

// soonWantedNotice is the superadmin DM for a coming-soon tap. Plain text, English, same shape as
// the other staff notices: opsnotify sends with no parse mode.
func soonWantedNotice(label string, user identity.User) string {
	var b strings.Builder
	b.WriteString(label + " download wanted\n")
	fmt.Fprintf(&b, "id: %d", user.TGUserID)
	if name := strings.TrimSpace(user.Name); name != "" {
		fmt.Fprintf(&b, "\nname: %s", name)
	}
	if username := strings.TrimPrefix(strings.TrimSpace(user.Username), "@"); username != "" {
		fmt.Fprintf(&b, "\n@%s", username)
	}
	return b.String()
}

// dlTitleFor names the version on offer, so somebody who already has a build can tell whether this
// one is newer without downloading it. Every platform on the channel is from the same release, so
// the first version found is the release.
func dlTitleFor(locale string, available map[string]string) string {
	for _, p := range dlPlatformOrder {
		if v := available[p.platform]; v != "" {
			return dlTitle.pick(locale) + "\n\n<code>" + v + "</code>"
		}
	}
	return dlTitle.pick(locale)
}

func (b *bot) showDownload(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.showDownloadLead(ctx, s, locale, "", log)
}

func (b *bot) showDownloadLead(ctx context.Context, s screen, locale, lead string, log *slog.Logger) {
	available := b.availableReleases(ctx, log)
	if len(available) == 0 {
		b.show(ctx, s, joinDownloadLead(lead, dlEmpty.pick(locale)), downloadKeyboard(locale, nil), log)
		return
	}
	b.show(ctx, s, joinDownloadLead(lead, dlTitleFor(locale, available)), downloadKeyboard(locale, available), log)
}

func joinDownloadLead(lead, body string) string {
	if strings.TrimSpace(lead) == "" {
		return body
	}
	return strings.TrimSpace(lead) + "\n\n" + body
}

// availableReleases is best-effort: a lookup that fails shows the empty screen, which is honest
// ("nothing to offer right now") and beats a picker whose every button then fails.
func (b *bot) availableReleases(ctx context.Context, log *slog.Logger) map[string]string {
	if b.appChannelID == 0 || b.releases == nil {
		return nil
	}
	available, err := b.releases.AppReleasePlatforms(ctx)
	if err != nil {
		log.Error("app release platforms", "err", err)
		return nil
	}
	return available
}

func (b *bot) routeDownloadCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch {
	case q.Data == cbMenuDownload:
		b.toast(ctx, q, "")
		// New message: keep Home, open the glass picker under it.
		b.showDownload(ctx, screen{chatID: s.chatID}, locale, log)
		return true
	case strings.HasPrefix(q.Data, cbDlPrefix):
		b.sendAppRelease(ctx, q, s.chatID, strings.TrimPrefix(q.Data, cbDlPrefix), user, locale, log)
		return true
	default:
		return false
	}
}

func (b *bot) sendAppRelease(
	ctx context.Context, q *tg.CallbackQuery, chatID int64, platform string, user identity.User, locale string, log *slog.Logger,
) {
	if soon, ok := dlComingSoon[platform]; ok {
		b.toast(ctx, q, soon.toast.pick(locale))
		if b.notify != nil {
			b.notify.NotifyAdmins(ctx, soonWantedNotice(soon.label, user))
		}
		return
	}
	if b.appChannelID == 0 || b.releases == nil {
		b.toast(ctx, q, dlToastNoChannel.pick(locale))
		return
	}
	channelID, messageID, ok, err := b.releases.AppRelease(ctx, platform)
	if err != nil {
		log.Error("app release lookup", "err", err, "platform", platform)
		b.toast(ctx, q, failed.pick(locale))
		return
	}
	if !ok || channelID == 0 || messageID == 0 {
		b.toast(ctx, q, dlToastMissing.pick(locale))
		return
	}
	b.toast(ctx, q, dlToastSend.pick(locale))
	if _, err := b.client.CopyMessage(ctx, chatID, channelID, messageID, ""); err != nil {
		log.Error("copy app release", "err", err, "platform", platform, "src", messageID)
		if _, sendErr := b.client.SendHTML(ctx, chatID, failed.pick(locale)); sendErr != nil {
			log.Error("download failure notice", "err", sendErr)
		}
		return
	}
	if _, err := b.client.SendHTML(ctx, chatID, dlSent.pick(locale)); err != nil {
		log.Error("download sent notice", "err", err)
	}
}

// handleAppReleasePost registers a binary posted to RELEASE_CHANNEL and clears out the one it replaces.
//
// Each track (stable, nightly) carries one release per platform. A superseded upload of the same
// track is deleted as soon as its replacement is registered — the other track stays, so a nightly
// post cannot retire the stable file main-bot Download still copies. main-bot is an admin there,
// which is what lets it delete. Note that Telegram never reports a bot's own messages back to it,
// so the poster must be a different bot (exampleWorkerBot, per reader/provision_bots.py); main-bot
// posting its own binaries would leave `app_releases` permanently empty. Nightly posts are stored
// for the in-app beta path and are never offered on Download.
func (b *bot) handleAppReleasePost(ctx context.Context, msg *tg.Message) {
	if b.appChannelID == 0 || b.releases == nil || msg == nil || msg.Chat.ID != b.appChannelID {
		return
	}
	name := ""
	if msg.Document != nil {
		name = msg.Document.FileName
	}
	plats := platformsForFile(name, msg.Caption)
	if len(plats) == 0 {
		// Says so out loud: the usual cause of a missing Download button is a caption whose
		// hashtag was mistyped, and silence there is very hard to debug from the outside.
		b.log.Info("app channel post offers no platform", "msg", msg.ID, "file", name,
			"caption", firstLine(msg.Caption))
		return
	}
	version := versionFromCaption(msg.Caption)
	track := releaseTrack(version)
	var replaced []int64
	for _, p := range plats {
		prev, err := b.releases.UpsertAppRelease(ctx, p, track, msg.Chat.ID, msg.ID, version, name)
		if err != nil {
			b.log.Error("upsert app release", "err", err, "platform", p)
			continue
		}
		b.log.Info("app release registered", "platform", p, "track", track, "version", version, "msg", msg.ID, "file", name)
		replaced = append(replaced, prev)
	}
	for _, id := range supersededIDs(replaced, msg.ID) {
		if err := b.client.DeleteMessage(ctx, b.appChannelID, id); err != nil {
			b.log.Warn("delete superseded app release", "err", err, "msg", id)
		}
	}
}

// supersededIDs is the set of channel messages this post retires: the ids the updated platforms
// used to point at, minus zeroes (that platform had no build) and minus the new post itself (a
// re-registration of the same message must not delete it). One upload can serve two platforms — an
// armeabi-v7a APK is both old-phone and TV — so the same id arrives twice and is collapsed to one
// delete.
func supersededIDs(replaced []int64, current int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range replaced {
		if id == 0 || id == current || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// platformsForFile decides which download buttons a channel post feeds.
//
// Hashtags win: the caption is written by whoever posts the release, so `#windows` is a deliberate
// statement, while a file name is a build-system artefact that changes shape whenever packaging
// does. The file-name rules stay as the fallback for a post that carries no tags.
func platformsForFile(fileName, caption string) []string {
	if tagged := platformsFromTags(caption); len(tagged) > 0 {
		return tagged
	}
	blob := strings.ToLower(fileName + " " + caption)
	switch {
	case strings.Contains(blob, "arm64-v8a") && strings.Contains(blob, ".apk"):
		return []string{platAndroidNew}
	case strings.Contains(blob, "armeabi-v7a") && strings.Contains(blob, ".apk"):
		// 32-bit ABI: old phones, and the Android TV boxes that reject arm64.
		return []string{platAndroidOld, platAndroidTV}
	case strings.Contains(blob, "setup.exe") || (strings.Contains(blob, "windows") && strings.Contains(blob, "installer")):
		return []string{platWindows}
	case strings.Contains(blob, ".ipa"):
		return []string{platIOS}
	case strings.Contains(blob, ".dmg"):
		return []string{platMacOS}
	case strings.Contains(blob, ".appimage") && !strings.Contains(blob, ".zsync"):
		return []string{platLinux}
	default:
		return nil
	}
}

// tagPlatforms maps a caption hashtag to the buttons it feeds. `#android` / `#arm64` is the new
// phone build only. TV shares the 32-bit APK with old Android (`#android_old`, `#armeabi`) — cheap
// boxes often have no arm64. Both spellings of each tag are accepted (`#androidtv`, `#android_tv`)
// so a human posting by hand does not have to remember which one the parser prefers.
var tagPlatforms = map[string][]string{
	"android":     {platAndroidNew},
	"arm64":       {platAndroidNew},
	"androidold":  {platAndroidOld, platAndroidTV},
	"android_old": {platAndroidOld, platAndroidTV},
	"armeabi":     {platAndroidOld, platAndroidTV},
	"armv7":       {platAndroidOld, platAndroidTV},
	"androidtv":   {platAndroidTV},
	"android_tv":  {platAndroidTV},
	"tv":          {platAndroidTV},
	"windows":     {platWindows},
	"linux":       {platLinux},
	"macos":       {platMacOS},
	"mac":         {platMacOS},
	"ios":         {platIOS},
}

// platformsFromTags reads the `#tags` out of a caption, in dlPlatformOrder so the result does not
// depend on how the caption was typed. An unknown tag is ignored: captions carry prose tags too.
func platformsFromTags(caption string) []string {
	found := map[string]bool{}
	for _, field := range strings.Fields(strings.ToLower(caption)) {
		if !strings.HasPrefix(field, "#") {
			continue
		}
		tag := strings.Trim(field[1:], ".,:;!?()[]·")
		for _, p := range tagPlatforms[tag] {
			found[p] = true
		}
	}
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, len(found))
	for _, p := range dlPlatformOrder {
		if found[p.platform] {
			out = append(out, p.platform)
		}
	}
	return out
}

// firstLine keeps a log line one line long: a release caption carries prose, tags and newlines.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

// releaseTrack is which shelf a channel post belongs on. A version with "-nightly" is the beta
// build. Download only lists stable; nightly is copied later by /appupdate for beta testers.
func releaseTrack(version string) string {
	if strings.Contains(strings.ToLower(version), "-nightly") {
		return "nightly"
	}
	return "stable"
}

func versionFromCaption(caption string) string {
	// "Sushi 1.1.150-nightly · Android arm64-v8a"
	s := strings.TrimSpace(caption)
	if s == "" {
		return ""
	}
	fields := strings.Fields(s)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "sushi") {
		return strings.Trim(fields[1], "·")
	}
	return ""
}
