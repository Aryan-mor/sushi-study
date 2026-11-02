package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// clearEnv wipes every variable LoadBackup reads, so one test cannot see another's leftovers.
// t.Setenv already restores the previous value on cleanup; this only has to guarantee a known
// starting point, which is why it runs before every subtest below.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"SUSHI_ENV", "DATABASE_URL", "LIBRARY_DATABASE_URL", "CHANNELS", "BACKUP_BOTS",
		"API_BOTS", "MAIN_BOTS", "MAIN_BOT_TOKEN", "MAIN_BOT_USERNAME", "SUSHI_ALLOW_UNBOUND",
		"DELIVERY_BOTS", "DELIVERY_RATE_PER_HOUR", "DELIVERY_MAX_CONCURRENT", "SEARCH_RATE_PER_HOUR",
		"INIT_BOTS", "SUPERADMIN_IDS", "ADMIN_IDS", "FOLLOW_NOTIFY_DEBOUNCE",
		"RELEASE_CHANNEL", "ANNOUNCE_DELAY", "NEWS_TAP_QUOTA_PER_HOUR", "SUSHI_IMAGE_TAG",
		"CATALOG_BOT", "TMDB_API_KEY", "PERSONAL_INTAKE", "PERSONAL_UPLOADS_PER_DAY",
		"LLM_PROVIDER", "LLM_CLASSIFY_MODEL", "LLM_REFEREE_MODEL", "LLM_VERIFIER_MODEL",
		"LLM_FALLBACK_MODEL", "GOOGLE_VERTEX_PROJECT", "GOOGLE_VERTEX_LOCATION", "GOOGLE_VERTEX_SA_JSON",
		"OPENROUTER_API_KEY", "OPENROUTER_MODEL", "OPENROUTER_FALLBACK_MODEL", "OPENROUTER_PROVIDER",
	} {
		t.Setenv(k, "")
	}
}

const (
	threeChannels = "clone|-1009900000009,backup1|-1009900000007,backup2|-1009900000008"
	threeBots     = "backup1_bot|111:AAA|-1009900000007,backup2_bot|222:BBB|-1009900000008"
)

func TestLoadBackupBindsEachBotToItsRole(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", threeBots)

	cfg, err := config.LoadBackup("")
	if err != nil {
		t.Fatalf("LoadBackup: %v", err)
	}
	if cfg.CloneID != -1009900000009 {
		t.Errorf("CloneID = %d", cfg.CloneID)
	}
	if len(cfg.Slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(cfg.Slots))
	}
	byRole := map[string]config.BackupSlot{}
	for _, s := range cfg.Slots {
		byRole[s.Role] = s
	}
	if byRole["backup1"].ChannelID != -1009900000007 {
		t.Errorf("backup1 channel = %d", byRole["backup1"].ChannelID)
	}
	if byRole["backup2"].Username != "backup2_bot" {
		t.Errorf("backup2 username = %q", byRole["backup2"].Username)
	}
}

func TestLoadBackupPointsTheBackup1BotAtPersonalBackup(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels+",personal_clone|-1009900000004,personal_backup|-1009900000011")
	t.Setenv("BACKUP_BOTS", threeBots)

	cfg, err := config.LoadBackup("")
	if err != nil {
		t.Fatalf("LoadBackup: %v", err)
	}
	pb := cfg.PersonalBackup
	if pb == nil {
		t.Fatal("PersonalBackup = nil, want the backup1 bot bound to personal_backup")
	}
	if pb.Username != "backup1_bot" || pb.ChannelID != -1009900000011 || pb.Role != "personal_backup" {
		t.Errorf("PersonalBackup = %+v", *pb)
	}
	// The personal channels never become catalog backup slots.
	if len(cfg.Slots) != 2 {
		t.Errorf("got %d slots, want the two catalog backups only", len(cfg.Slots))
	}
}

func TestLoadBackupWithoutPersonalBackup(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", threeBots)

	cfg, err := config.LoadBackup("")
	if err != nil {
		t.Fatalf("LoadBackup: %v", err)
	}
	if cfg.PersonalBackup != nil {
		t.Errorf("PersonalBackup = %+v, want nil", *cfg.PersonalBackup)
	}
}

func TestLoadBackupRejectsMissingBackupBots(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "BACKUP_BOTS is empty") {
		t.Fatalf("err = %v, want a complaint about BACKUP_BOTS", err)
	}
}

func TestLoadBackupRejectsMissingChannels(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("BACKUP_BOTS", threeBots)

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "CHANNELS is empty") {
		t.Fatalf("err = %v, want a complaint about CHANNELS", err)
	}
}

func TestLoadBackupRejectsABotBoundToTheCloneChannel(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", "sneaky_bot|111:AAA|-1009900000009")

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "bound to the clone channel") {
		t.Fatalf("err = %v, want a complaint about the clone channel", err)
	}
}

func TestLoadBackupRejectsABotForAChannelNotInChannels(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", "stray_bot|111:AAA|-1009900000013")

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "is not in CHANNELS") {
		t.Fatalf("err = %v, want a complaint that the channel is not in CHANNELS", err)
	}
}

func TestLoadBackupRejectsTwoBotsForOneChannel(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", "backup1_bot|111:AAA|-1009900000007,also_backup1_bot|222:BBB|-1009900000007")

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "bound to both") {
		t.Fatalf("err = %v, want a complaint about a shared channel", err)
	}
}

// A backup channel CHANNELS names but BACKUP_BOTS never fills would previously boot cleanly and
// silently receive nothing, forever. That must now be a startup error (R-GO-9).
func TestLoadBackupRejectsABackupChannelWithNoBot(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", "backup1_bot|111:AAA|-1009900000007")

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "backup2") || !strings.Contains(err.Error(), "has no bot") {
		t.Fatalf("err = %v, want a complaint that backup2 has no bot", err)
	}
}

func TestLoadBackupRejectsAnUnknownEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("CHANNELS", threeChannels)
	t.Setenv("BACKUP_BOTS", threeBots)
	t.Setenv("SUSHI_ENV", "production") // not one of dev/staging/prod

	_, err := config.LoadBackup("")
	if err == nil || !strings.Contains(err.Error(), "is not dev, staging or prod") {
		t.Fatalf("err = %v, want a complaint about SUSHI_ENV", err)
	}
}

func TestLoadAPIDDefaultsTheDeliveryRateLimits(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("API_BOTS", "api1_bot|111:AAA")

	cfg, err := config.LoadAPID("")
	if err != nil {
		t.Fatalf("LoadAPID: %v", err)
	}
	if cfg.DeliveriesPerHour != 30 {
		t.Errorf("DeliveriesPerHour = %d, want the R-SEC-11 default of 30", cfg.DeliveriesPerHour)
	}
	if cfg.MaxConcurrentDeliveries != 2 {
		t.Errorf("MaxConcurrentDeliveries = %d, want the R-SEC-11 default of 2", cfg.MaxConcurrentDeliveries)
	}
	if cfg.SearchPerHour != 60 {
		t.Errorf("SearchPerHour = %d, want the R-SEC-11 default of 60", cfg.SearchPerHour)
	}
}

func TestLoadAPIDReadsTheDeliveryRateLimitsFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("API_BOTS", "api1_bot|111:AAA")
	t.Setenv("DELIVERY_RATE_PER_HOUR", "60")
	t.Setenv("DELIVERY_MAX_CONCURRENT", "5")
	t.Setenv("SEARCH_RATE_PER_HOUR", "120")

	cfg, err := config.LoadAPID("")
	if err != nil {
		t.Fatalf("LoadAPID: %v", err)
	}
	if cfg.DeliveriesPerHour != 60 {
		t.Errorf("DeliveriesPerHour = %d, want 60", cfg.DeliveriesPerHour)
	}
	if cfg.MaxConcurrentDeliveries != 5 {
		t.Errorf("MaxConcurrentDeliveries = %d, want 5", cfg.MaxConcurrentDeliveries)
	}
	if cfg.SearchPerHour != 120 {
		t.Errorf("SearchPerHour = %d, want 120", cfg.SearchPerHour)
	}
}

// A rate limit of zero would silently forbid every delivery -- exactly the misconfiguration R-GO-9
// wants loud rather than discovered by a user reporting playback is broken.
func TestLoadAPIDRejectsAZeroOrNegativeDeliveryRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"per-hour zero", "DELIVERY_RATE_PER_HOUR", "0"},
		{"per-hour negative", "DELIVERY_RATE_PER_HOUR", "-1"},
		{"per-hour not a number", "DELIVERY_RATE_PER_HOUR", "many"},
		{"concurrent zero", "DELIVERY_MAX_CONCURRENT", "0"},
		{"search per-hour zero", "SEARCH_RATE_PER_HOUR", "0"},
		{"search per-hour negative", "SEARCH_RATE_PER_HOUR", "-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("DATABASE_URL", "postgres://env")
			t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
			t.Setenv("API_BOTS", "api1_bot|111:AAA")
			t.Setenv(tc.key, tc.value)

			if _, err := config.LoadAPID(""); err == nil {
				t.Fatalf("LoadAPID accepted %s=%s", tc.key, tc.value)
			}
		})
	}
}

func TestLoadDeliveryReadsThePool(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("DELIVERY_BOTS", "dl1_bot|111:AAA,dl2_bot|222:BBB")

	cfg, err := config.LoadDelivery("")
	if err != nil {
		t.Fatalf("LoadDelivery: %v", err)
	}
	if cfg.Bots == nil || cfg.Bots.Len() != 2 {
		t.Fatalf("Bots = %v, want 2 bots", cfg.Bots)
	}
	// Delivery bots are not bound to a channel (unlike BACKUP_BOTS): CHANNELS plays no part here.
	if _, ok := cfg.Bots.ByUsername("dl1_bot"); !ok {
		t.Error("dl1_bot not found in the pool")
	}
}

func TestLoadDeliveryRejectsMissingBots(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")

	_, err := config.LoadDelivery("")
	if err == nil || !strings.Contains(err.Error(), "DELIVERY_BOTS is empty") {
		t.Fatalf("err = %v, want a complaint about DELIVERY_BOTS", err)
	}
}

// Unlike DELIVERY_BOTS, an absent API_BOTS is not a misconfiguration: the Ready push it enables
// has its own polling fallback (docs/02 §8), so deliveryd must still start without it.
func TestLoadDeliveryDefaultsAPIBotsToNil(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("DELIVERY_BOTS", "dl1_bot|111:AAA")

	cfg, err := config.LoadDelivery("")
	if err != nil {
		t.Fatalf("LoadDelivery: %v", err)
	}
	if cfg.APIBots != nil {
		t.Errorf("APIBots = %v, want nil when API_BOTS is unset", cfg.APIBots)
	}
}

func TestLoadDeliveryReadsAPIBotsWhenPresent(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("DELIVERY_BOTS", "dl1_bot|111:AAA")
	t.Setenv("API_BOTS", "api1_bot|222:BBB")

	cfg, err := config.LoadDelivery("")
	if err != nil {
		t.Fatalf("LoadDelivery: %v", err)
	}
	if cfg.APIBots == nil || cfg.APIBots.Len() != 1 {
		t.Fatalf("APIBots = %v, want 1 bot", cfg.APIBots)
	}
}

func TestLoadDeliveryRejectsAMalformedAPIBots(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://library")
	t.Setenv("DELIVERY_BOTS", "dl1_bot|111:AAA")
	t.Setenv("API_BOTS", "not-a-valid-entry")

	if _, err := config.LoadDelivery(""); err == nil {
		t.Fatal("LoadDelivery accepted a malformed API_BOTS")
	}
}

func TestLoadMainBotReadsThePackedList(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.Token != "111:AAA" {
		t.Errorf("Token = %q, want 111:AAA", cfg.Token)
	}
	if cfg.Username != "exampleMainBot" {
		t.Errorf("Username = %q, want exampleMainBot", cfg.Username)
	}
	if cfg.LibraryURL != "postgres://lib" {
		t.Errorf("LibraryURL = %q, want postgres://lib", cfg.LibraryURL)
	}
	if cfg.InitBotUsername != "exampleInitBot" {
		t.Errorf("InitBotUsername = %q, want exampleInitBot", cfg.InitBotUsername)
	}
	if cfg.SupportBotUsername != "exampleSupportBot" {
		t.Errorf("SupportBotUsername = %q, want default exampleSupportBot", cfg.SupportBotUsername)
	}
	if cfg.AppChannelID != 0 {
		t.Errorf("AppChannelID = %d, want 0 when RELEASE_CHANNEL unset", cfg.AppChannelID)
	}
}

func mainBotBaseEnv(t *testing.T) {
	t.Helper()
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
}

// Personal intake is on wherever CHANNELS names a personal_clone, and PERSONAL_INTAKE=off closes it
// (ADR 0037 §1: how dev's testing window ends).
func TestLoadMainBotPersonalIntake(t *testing.T) {
	const chans = "clone|-1009900000009,personal_clone|-1009900000004"
	for _, tc := range []struct {
		name, channels, intake string
		want                   int64
	}{
		{"named and on by default", chans, "", -1009900000004},
		{"named and on", chans, "on", -1009900000004},
		{"named but off", chans, "off", 0},
		{"not named", "clone|-1009900000009", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mainBotBaseEnv(t)
			t.Setenv("CHANNELS", tc.channels)
			t.Setenv("PERSONAL_INTAKE", tc.intake)
			cfg, err := config.LoadMainBot("")
			if err != nil {
				t.Fatalf("LoadMainBot: %v", err)
			}
			if cfg.PersonalCloneID != tc.want {
				t.Errorf("PersonalCloneID = %d, want %d", cfg.PersonalCloneID, tc.want)
			}
			if cfg.PersonalUploadsPerDay != 20 {
				t.Errorf("PersonalUploadsPerDay = %d, want the default 20", cfg.PersonalUploadsPerDay)
			}
		})
	}
}

func TestLoadMainBotRejectsATypoInPersonalIntake(t *testing.T) {
	mainBotBaseEnv(t)
	t.Setenv("CHANNELS", "clone|-1009900000009,personal_clone|-1009900000004")
	t.Setenv("PERSONAL_INTAKE", "of")
	if _, err := config.LoadMainBot(""); err == nil || !strings.Contains(err.Error(), "PERSONAL_INTAKE") {
		t.Fatalf("err = %v, want a PERSONAL_INTAKE complaint", err)
	}
}

// Not t.Parallel: clearEnv uses t.Setenv, which Go refuses to combine with a parallel test — the
// rest of the LoadMainBot tests are serial for the same reason.
func TestLoadMainBotParsesAppChannel(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
	t.Setenv("RELEASE_CHANNEL", "app|-1009900000001")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.AppChannelID != -1009900000001 {
		t.Errorf("AppChannelID = %d, want -1009900000001", cfg.AppChannelID)
	}
}

// SUSHI_IMAGE_TAG is what main-bot announces after a deploy (ADR 0016). A checkout that never ran
// the deploy script has none, which is how the announcement stays off in dev.
func TestLoadMainBotReadsTheReleaseTag(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.ReleaseTag != "" {
		t.Errorf("ReleaseTag = %q, want empty when SUSHI_IMAGE_TAG is unset", cfg.ReleaseTag)
	}

	t.Setenv("SUSHI_IMAGE_TAG", " 99c51b0abcde ")
	cfg, err = config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.ReleaseTag != "99c51b0abcde" {
		t.Errorf("ReleaseTag = %q, want the trimmed sha", cfg.ReleaseTag)
	}
}

// The announce channel is optional, unlike the clone: a checkout that only serves logins has no
// CHANNELS at all, and main-bot must still boot (ADR 0017 §2).
func TestLoadMainBotReadsTheNewsChannelFromChannels(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot with no CHANNELS: %v", err)
	}
	if cfg.NewsChannelID != 0 {
		t.Errorf("NewsChannelID = %d, want 0 when CHANNELS is unset", cfg.NewsChannelID)
	}
	if cfg.RequestChannelID != 0 {
		t.Errorf("RequestChannelID = %d, want 0 when CHANNELS is unset", cfg.RequestChannelID)
	}
	if cfg.AnnounceDelay != 10*time.Minute {
		t.Errorf("AnnounceDelay = %s, want the 10m default", cfg.AnnounceDelay)
	}
	if cfg.NewsTapQuotaPerHour != 60 {
		t.Errorf("NewsTapQuotaPerHour = %d, want the default 60", cfg.NewsTapQuotaPerHour)
	}

	// A CHANNELS with a clone but no news role is also fine — still nothing to announce to.
	t.Setenv("CHANNELS", threeChannels)
	cfg, err = config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot with no news role: %v", err)
	}
	if cfg.NewsChannelID != 0 {
		t.Errorf("NewsChannelID = %d, want 0 when CHANNELS names no news role", cfg.NewsChannelID)
	}
	if cfg.RequestChannelID != 0 {
		t.Errorf("RequestChannelID = %d, want 0 when CHANNELS names no requests role", cfg.RequestChannelID)
	}

	t.Setenv("CHANNELS", threeChannels+",news|-1009900000010")
	cfg, err = config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot with a news role: %v", err)
	}
	if cfg.NewsChannelID != -1009900000010 {
		t.Errorf("NewsChannelID = %d, want the channel CHANNELS names", cfg.NewsChannelID)
	}

	t.Setenv("CHANNELS", threeChannels+",requests|-1009900000002")
	cfg, err = config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot with a requests role: %v", err)
	}
	if cfg.RequestChannelID != -1009900000002 {
		t.Errorf("RequestChannelID = %d, want the channel CHANNELS names", cfg.RequestChannelID)
	}
}

// A typo in CHANNELS must not quietly turn announcements off (R-GO-9): a set value that will not
// parse is a boot failure, the same as it is for every other process that reads it.
func TestLoadMainBotRejectsMalformedChannels(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
	t.Setenv("CHANNELS", "news|not-an-id")

	if _, err := config.LoadMainBot(""); err == nil {
		t.Fatal("LoadMainBot accepted a CHANNELS that does not parse")
	}
}

func TestLoadMainBotReadsRequestTopics(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.RequestForum.Enabled() {
		t.Fatal("REQUEST_TOPICS unset should leave the forum override off")
	}

	t.Setenv("REQUEST_TOPICS", "open|11,reject|22,done|33")
	cfg, err = config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot with REQUEST_TOPICS: %v", err)
	}
	if cfg.RequestForum.Open != 11 || cfg.RequestForum.Reject != 22 || cfg.RequestForum.Done != 33 {
		t.Fatalf("RequestForum = %+v, want open 11 reject 22 done 33", cfg.RequestForum)
	}

	t.Setenv("REQUEST_TOPICS", "open|11,reject|22")
	if _, err := config.LoadMainBot(""); err == nil {
		t.Fatal("LoadMainBot accepted REQUEST_TOPICS missing done")
	}
}

func TestLoadMainBotStripsAtFromSupportHandle(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
	t.Setenv("SUPPORT_BOT_USERNAME", "@exampleSupportBot")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.SupportBotUsername != "exampleSupportBot" {
		t.Errorf("SupportBotUsername = %q, want exampleSupportBot without @", cfg.SupportBotUsername)
	}
}

func TestLoadMainBotParsesRoleLists(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
	// blanks and one unparseable entry are dropped, not fatal (ADR 0014 §D1).
	t.Setenv("SUPERADMIN_IDS", "42, 43 ,")
	t.Setenv("ADMIN_IDS", "7,notanid,9")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if want := []int64{42, 43}; !equalInt64(cfg.SuperadminIDs, want) {
		t.Errorf("SuperadminIDs = %v, want %v", cfg.SuperadminIDs, want)
	}
	if want := []int64{7, 9}; !equalInt64(cfg.AdminIDs, want) {
		t.Errorf("AdminIDs = %v, want %v", cfg.AdminIDs, want)
	}
}

func TestLoadMainBotAlwaysIncludesTheOwnerSuperadminOnceOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
	// owner id listed explicitly must not be duplicated.
	t.Setenv("SUPERADMIN_IDS", "1,5")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if want := []int64{1, 5}; !equalInt64(cfg.SuperadminIDs, want) {
		t.Errorf("SuperadminIDs = %v, want %v (no duplicate owner)", cfg.SuperadminIDs, want)
	}
}

func TestLoadMainBotRoleListsDefaultToJustTheOwner(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if want := []int64{}; !equalInt64(cfg.SuperadminIDs, want) {
		t.Errorf("SuperadminIDs = %v, want just the owner %v", cfg.SuperadminIDs, want)
	}
	if len(cfg.AdminIDs) != 0 {
		t.Errorf("AdminIDs = %v, want empty", cfg.AdminIDs)
	}
	if cfg.FollowNotifyDebounce != 15*time.Minute {
		t.Errorf("FollowNotifyDebounce = %s, want the 15m default", cfg.FollowNotifyDebounce)
	}
}

func TestLoadMainBotRejectsABadFollowDebounce(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")
	t.Setenv("FOLLOW_NOTIFY_DEBOUNCE", "soon")

	if _, err := config.LoadMainBot(""); err == nil || !strings.Contains(err.Error(), "FOLLOW_NOTIFY_DEBOUNCE") {
		t.Fatalf("err = %v, want a complaint about FOLLOW_NOTIFY_DEBOUNCE", err)
	}
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLoadOpsNotifyReadsMainBotsToken(t *testing.T) {
	clearEnv(t)
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("SUPERADMIN_IDS", "42")

	got := config.LoadOpsNotify()
	if got.BotToken != "111:AAA" {
		t.Errorf("BotToken = %q, want 111:AAA", got.BotToken)
	}
	if want := []int64{42}; !equalInt64(got.AdminIDs, want) {
		t.Errorf("AdminIDs = %v, want %v (owner always included)", got.AdminIDs, want)
	}
}

func TestLoadOpsNotifyIncludesTheOwnerEvenWithNoSuperadminIDs(t *testing.T) {
	clearEnv(t)
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")

	got := config.LoadOpsNotify()
	if want := []int64{}; !equalInt64(got.AdminIDs, want) {
		t.Errorf("AdminIDs = %v, want just the owner %v", got.AdminIDs, want)
	}
}

func TestLoadMainBotRejectsMissingPool(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")

	_, err := config.LoadMainBot("")
	if err == nil || !strings.Contains(err.Error(), "MAIN_BOTS is empty") {
		t.Fatalf("err = %v, want a complaint about MAIN_BOTS", err)
	}
}

func TestLoadInitBotReadsThePackedListAndMainBotUsername(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("INIT_BOTS", "exampleInitBot|111:AAA")
	t.Setenv("MAIN_BOTS", "exampleMainBot|999:ZZZ")
	t.Setenv("LOGIN_POST_CHANNEL", "exampleLoginChannel|-1009900000014")

	cfg, err := config.LoadInitBot("")
	if err != nil {
		t.Fatalf("LoadInitBot: %v", err)
	}
	if cfg.Token != "111:AAA" {
		t.Errorf("Token = %q, want 111:AAA", cfg.Token)
	}
	if cfg.Username != "exampleInitBot" {
		t.Errorf("Username = %q, want exampleInitBot", cfg.Username)
	}
	if cfg.MainBotUsername != "exampleMainBot" {
		t.Errorf("MainBotUsername = %q, want exampleMainBot", cfg.MainBotUsername)
	}
	if cfg.LoginChannelUsername != "exampleLoginChannel" || cfg.LoginChannelID != -1009900000014 {
		t.Errorf("login channel = %s %d", cfg.LoginChannelUsername, cfg.LoginChannelID)
	}
}

func TestLoadInitBotRejectsMissingPool(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")

	_, err := config.LoadInitBot("")
	if err == nil || !strings.Contains(err.Error(), "INIT_BOTS is empty") {
		t.Fatalf("err = %v, want a complaint about INIT_BOTS", err)
	}
}

func TestLoadInitBotRejectsMissingDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("INIT_BOTS", "exampleInitBot|111:AAA")

	_, err := config.LoadInitBot("")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is empty") {
		t.Fatalf("err = %v, want a complaint about DATABASE_URL", err)
	}
}

func TestLoadDeliveryRejectsMissingDatabases(t *testing.T) {
	clearEnv(t)
	t.Setenv("DELIVERY_BOTS", "dl1_bot|111:AAA")

	_, err := config.LoadDelivery("")
	if err == nil {
		t.Fatal("LoadDelivery accepted no DATABASE_URL or LIBRARY_DATABASE_URL")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL is empty") || !strings.Contains(err.Error(), "LIBRARY_DATABASE_URL is empty") {
		t.Errorf("err = %v, want complaints about both database URLs", err)
	}
}
