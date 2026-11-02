package config_test

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func TestLoadMainBotReadsOptionalCatalogBot(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("LIBRARY_DATABASE_URL", "postgres://lib")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	t.Setenv("INIT_BOTS", "exampleInitBot|222:BBB")

	cfg, err := config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot: %v", err)
	}
	if cfg.CatalogBotUsername != "" {
		t.Errorf("CatalogBotUsername = %q, want empty when CATALOG_BOT is unset", cfg.CatalogBotUsername)
	}

	t.Setenv("CATALOG_BOT", "exampleCatalogBot|333:CCC")
	cfg, err = config.LoadMainBot("")
	if err != nil {
		t.Fatalf("LoadMainBot with CATALOG_BOT: %v", err)
	}
	if cfg.CatalogBotUsername != "exampleCatalogBot" {
		t.Errorf("CatalogBotUsername = %q", cfg.CatalogBotUsername)
	}
}

func TestLoadCatalogBotRequiresTheToken(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("MAIN_BOTS", "exampleMainBot|111:AAA")
	if _, err := config.LoadCatalogBot(""); err == nil {
		t.Fatal("LoadCatalogBot accepted an empty CATALOG_BOT")
	}

	t.Setenv("CATALOG_BOT", "exampleCatalogBot|333:CCC")
	cfg, err := config.LoadCatalogBot("")
	if err != nil {
		t.Fatalf("LoadCatalogBot: %v", err)
	}
	if cfg.Username != "exampleCatalogBot" || cfg.Token != "333:CCC" {
		t.Errorf("got %s / %s", cfg.Username, cfg.Token)
	}
	if cfg.MainBotUsername != "exampleMainBot" {
		t.Errorf("MainBotUsername = %q", cfg.MainBotUsername)
	}
	if cfg.RequestsPerDay != 10 {
		t.Errorf("RequestsPerDay = %d, want the default 10", cfg.RequestsPerDay)
	}
}
