package config

import (
	"fmt"
	"os"
	"strings"
)

// CatalogBot is the human-facing download bot (ADR 0022).
type CatalogBot struct {
	Env Env
	// Access says whether strangers may register (ADR 0039).
	Access          Access
	DatabaseURL     string
	LibraryURL      string
	Token           string
	Username        string
	MainBotUsername string
	TMDBAPIKey      string
	RequestsPerDay  int
	// LLM translates a title synopsis into Persian for the caption (ADR 0032). Optional:
	// without it the caption keeps TMDB's text.
	LLM            LLM
	TranslateModel string
}

// LoadCatalogBot reads CATALOG_BOT. Unlike main-bot, an empty value is a boot failure: this
// process has nothing else to do.
func LoadCatalogBot(envFile string) (CatalogBot, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return CatalogBot{}, err
		}
	}

	cfg := CatalogBot{
		Env:         Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LibraryURL:  os.Getenv("LIBRARY_DATABASE_URL"),
		TMDBAPIKey:  strings.TrimSpace(os.Getenv("TMDB_API_KEY")),
	}
	cfg.LLM = loadIndexerLLM()
	cfg.TranslateModel = translateModel(cfg.LLM)

	var problems []string
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is empty")
	}
	problems = append(problems, envProblems(cfg.Env)...)
	cfg.Access, problems = appendAccess(problems)

	bot, botProblems := firstPackedBot("CATALOG_BOT", os.Getenv("CATALOG_BOT"))
	problems = append(problems, botProblems...)
	cfg.Token = bot.Token
	cfg.Username = bot.Username

	mainBot, _ := firstPackedBot("MAIN_BOTS", os.Getenv("MAIN_BOTS"))
	cfg.MainBotUsername = telegramUsername(mainBot.Username)

	caps, capProblems := loadAbuseCaps()
	problems = append(problems, capProblems...)
	cfg.RequestsPerDay = caps.requestsPerDay

	if len(problems) > 0 {
		return CatalogBot{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// optionalPackedUsername reads a packed username|token list and keeps only the handle. Empty is
// fine (main-bot still serves logins without a catalog bot). A set-but-unparseable value is fatal.
func optionalPackedUsername(secretName, raw string) (string, []string) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	bot, problems := firstPackedBot(secretName, raw)
	return telegramUsername(bot.Username), problems
}
