package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Config struct {
	Twitch struct {
		Channel      string `json:"channel"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"twitch"`
	Telegram struct {
		BotToken string `json:"bot_token"`
		ChatID   *int64 `json:"chat_id"`
		ThreadID *int   `json:"thread_id"`
	} `json:"telegram"`
	Language       string `json:"language"`
	CheckInterval  int    `json:"check_interval_seconds"`
	UpdateInterval int    `json:"update_interval_minutes"`
	SetupCompleted bool   `json:"setup_completed"`
}

type Localization struct {
	StartedStreaming string
	IsLive           string
	StreamEnded      string
	ButtonText       string
	Peak             string
	Viewers          string
	Avg              string
	Clips            string
	Growing          string
	Steady           string
	Dropping         string
}

type ViewerDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"`
}

type StreamSession struct {
	MessageID     int
	StartTime     time.Time
	Game          string
	Title         string
	Tags          []string
	BroadcasterID string
	ViewerHistory []ViewerDataPoint
	UpdateCounter int
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if e := os.Getenv("TWITCH_CLIENT_ID"); e != "" {
		cfg.Twitch.ClientID = e
	}
	if e := os.Getenv("TWITCH_CLIENT_SECRET"); e != "" {
		cfg.Twitch.ClientSecret = e
	}
	if e := os.Getenv("TELEGRAM_BOT_TOKEN"); e != "" {
		cfg.Telegram.BotToken = e
	}

	if cfg.UpdateInterval == 0 {
		cfg.UpdateInterval = 5
	}
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 60
	}
	if cfg.Language == "" {
		cfg.Language = "ru"
	}

	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func getLocalization(lang string) Localization {
	switch lang {
	case "en":
		return Localization{
			StartedStreaming: "LIVE",
			IsLive:           "LIVE",
			StreamEnded:      "OFFLINE",
			ButtonText:       "Watch",
			Peak:             "peak",
			Viewers:          "viewers",
			Avg:              "avg",
			Clips:            "clips",
			Growing:          "growing",
			Steady:           "steady",
			Dropping:         "dropping",
		}
	case "ru":
		return Localization{
			StartedStreaming: "LIVE",
			IsLive:           "LIVE",
			StreamEnded:      "OFFLINE",
			ButtonText:       "Смотреть",
			Peak:             "пик",
			Viewers:          "зрителей",
			Avg:              "среднее",
			Clips:            "клипов",
			Growing:          "растёт",
			Steady:           "стабильно",
			Dropping:         "падает",
		}
	default:
		return getLocalization("en")
	}
}

func calculateAverage(history []ViewerDataPoint) int {
	if len(history) == 0 {
		return 0
	}
	sum := 0
	for _, p := range history {
		sum += p.Count
	}
	return sum / len(history)
}

func getMaxViewers(history []ViewerDataPoint) int {
	if len(history) == 0 {
		return 0
	}
	max := history[0].Count
	for _, p := range history {
		if p.Count > max {
			max = p.Count
		}
	}
	return max
}

func main() {
	configPath := "config.json"
	setupFlag := flag.Bool("setup", false, "Run interactive setup and exit")
	flag.Parse()

	if *setupFlag {
		if err := setupInteractive(configPath, true); err != nil {
			slog.Error("setup failed", "error", err)
			os.Exit(1)
		}
		fmt.Println("Setup completed successfully")
		os.Exit(0)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Println("No config file found. Starting interactive setup...")
		fmt.Println()
		if err := setupInteractive(configPath, false); err != nil {
			slog.Error("setup failed", "error", err)
			os.Exit(1)
		}
		cfg, err = loadConfig(configPath)
		if err != nil {
			slog.Error("failed to reload config", "error", err)
			os.Exit(1)
		}
	}

	if !cfg.SetupCompleted {
		fmt.Println("Setup incomplete. Running interactive setup...")
		fmt.Println()
		if err := setupInteractive(configPath, true); err != nil {
			slog.Error("setup failed", "error", err)
			os.Exit(1)
		}
		cfg, err = loadConfig(configPath)
		if err != nil {
			slog.Error("failed to reload config", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("starting monitor")
	monitorLoop(ctx, cfg)
}
