package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type TelegramUpdate struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		MessageID       int  `json:"message_id"`
		MessageThreadID *int `json:"message_thread_id"`
		Chat            struct {
			ID       int64  `json:"id"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			Username string `json:"username"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

type TelegramBotInfo struct {
	Username string `json:"username"`
}

func setupInteractive(configPath string, isReconfigure bool) error {
	reader := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	var cfg *Config
	if isReconfigure {
		var err error
		cfg, err = loadConfig(configPath)
		if err != nil {
			cfg = &Config{}
		}
	} else {
		cfg = &Config{}
	}

	fmt.Println()
	fmt.Println("Twitch Stream Monitor - Setup")
	fmt.Println()

	stepNum := 0
	totalSteps := 6

	if cfg.Twitch.ClientID == "" || cfg.Twitch.ClientSecret == "" {
		stepNum++
		fmt.Printf("[%d/%d] Twitch API Credentials\n", stepNum, totalSteps)
		fmt.Println("Get your credentials at: https://dev.twitch.tv/console")
		fmt.Println()

		for {
			clientID := promptString(reader, "Client ID", "")
			if clientID == "" {
				continue
			}
			clientSecret := promptString(reader, "Client Secret", "")
			if clientSecret == "" {
				continue
			}

			fmt.Print("Validating credentials... ")
			if err := validateTwitchCredentials(ctx, clientID, clientSecret); err != nil {
				fmt.Printf("Error: %v\n", err)
				if !promptRetry(reader) {
					return fmt.Errorf("setup cancelled")
				}
				continue
			}

			fmt.Println("OK")
			cfg.Twitch.ClientID = clientID
			cfg.Twitch.ClientSecret = clientSecret
			break
		}
		fmt.Println()
	}

	if cfg.Twitch.Channel == "" {
		stepNum++
		fmt.Printf("[%d/%d] Twitch Channel\n", stepNum, totalSteps)
		for {
			channel := promptString(reader, "Enter channel name", "")
			if channel == "" {
				continue
			}

			fmt.Print("Checking channel... ")
			if validateTwitchChannel(ctx, channel, cfg.Twitch.ClientID, cfg.Twitch.ClientSecret) {
				fmt.Println("OK")
				cfg.Twitch.Channel = channel
				break
			}
			fmt.Println("Error: Channel not found")
			if !promptRetry(reader) {
				return fmt.Errorf("setup cancelled")
			}
		}
		fmt.Println()
	}

	if cfg.Telegram.BotToken == "" {
		stepNum++
		fmt.Printf("[%d/%d] Telegram Bot\n", stepNum, totalSteps)
		fmt.Println("Create a bot via @BotFather on Telegram")
		fmt.Println()

		for {
			botToken := promptString(reader, "Bot Token", "")
			if botToken == "" || len(botToken) < 20 {
				fmt.Println("Error: Invalid format")
				continue
			}

			fmt.Print("Checking bot... ")
			botUsername, err := validateTelegramToken(ctx, botToken)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				if !promptRetry(reader) {
					return fmt.Errorf("setup cancelled")
				}
				continue
			}

			fmt.Printf("OK (Bot: @%s)\n", botUsername)
			cfg.Telegram.BotToken = botToken
			break
		}
		fmt.Println()
	}

	if cfg.Telegram.ChatID == nil {
		stepNum++
		fmt.Printf("[%d/%d] Chat Configuration\n", stepNum, totalSteps)
		fmt.Println()

		botUsername, _ := validateTelegramToken(ctx, cfg.Telegram.BotToken)

		fmt.Println("Choose setup method:")
		fmt.Println("1. Automatic - for groups (bot will detect chat ID)")
		fmt.Println("2. Manual - for channels (you provide chat ID)")
		fmt.Println()

		method := promptString(reader, "Select method (1/2)", "1")
		fmt.Println()

		var chatID int64
		var threadID *int

		if method == "2" {
			fmt.Println("To get your channel chat ID:")
			fmt.Println("1. Add your bot to the channel as administrator")
			fmt.Println("2. Forward any channel message to @userinfobot")
			fmt.Println("3. Copy the chat ID (number starting with -100)")
			fmt.Println()

			chatIDStr := promptString(reader, "Enter chat ID", "")
			if chatIDStr == "" {
				return fmt.Errorf("chat ID is required")
			}

			var err error
			chatID, err = strconv.ParseInt(chatIDStr, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid chat ID format: %w", err)
			}

			threadIDStr := promptString(reader, "Enter thread ID (optional, press Enter to skip)", "")
			if threadIDStr != "" {
				if v, err := strconv.Atoi(threadIDStr); err == nil {
					threadID = &v
				}
			}
		} else {
			if botUsername != "" {
				fmt.Printf("1. Add @%s to your group as administrator\n", botUsername)
			} else {
				fmt.Println("1. Add your bot to the group as administrator")
			}
			fmt.Println("2. Send 'SETUP' command in the group")
			fmt.Println()
			fmt.Print("Waiting for SETUP command... ")

			var err error
			chatID, threadID, err = waitForSetupCommand(ctx, cfg.Telegram.BotToken, 120)
			if err != nil {
				fmt.Printf("Error: %v\n\n", err)
				fmt.Println("Tip: If using a channel, restart and choose manual method (option 2)")
				return fmt.Errorf("setup failed: %w", err)
			}
		}

		fmt.Printf("OK (Chat ID: %d)\n\n", chatID)

		fmt.Print("Checking bot permissions... ")
		if err := checkBotPermissions(ctx, cfg.Telegram.BotToken, chatID); err != nil {
			fmt.Printf("Error\n\nMissing permissions: %v\n", err)
			fmt.Println("Please grant the bot permission to send messages")
			fmt.Println()
			fmt.Print("Waiting for permissions fix... ")
			if err := waitForPermissionsFix(ctx, cfg.Telegram.BotToken, chatID, 300); err != nil {
				fmt.Printf("Error: %v\n", err)
				return fmt.Errorf("setup failed: %w", err)
			}
		}

		fmt.Print("OK\n")
		cfg.Telegram.ChatID = &chatID
		cfg.Telegram.ThreadID = threadID
	}

	if cfg.Language == "" {
		stepNum++
		fmt.Printf("[%d/%d] Language\n", stepNum, totalSteps)
		lang := promptString(reader, "Select language (en/ru)", "en")
		if lang != "en" && lang != "ru" {
			lang = "en"
		}
		cfg.Language = lang
		fmt.Println()
	}

	if cfg.CheckInterval == 0 || cfg.UpdateInterval == 0 {
		stepNum++
		fmt.Printf("[%d/%d] Monitor Settings\n", stepNum, totalSteps)

		if cfg.CheckInterval == 0 {
			s := promptString(reader, "Check interval (seconds)", "60")
			v, err := strconv.Atoi(s)
			if err != nil || v <= 0 {
				v = 60
			}
			cfg.CheckInterval = v
		}

		if cfg.UpdateInterval == 0 {
			s := promptString(reader, "Update interval (minutes)", "5")
			v, err := strconv.Atoi(s)
			if err != nil || v <= 0 {
				v = 5
			}
			cfg.UpdateInterval = v
		}
		fmt.Println()
	}

	cfg.SetupCompleted = true
	if err := saveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Print("Configuration saved to config.json\n")
	return nil
}

func promptString(reader *bufio.Reader, prompt, defaultValue string) string {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultValue)
	} else {
		fmt.Printf("%s: ", prompt)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

func promptRetry(reader *bufio.Reader) bool {
	fmt.Print("Try again? (y/n): ")
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes" || input == ""
}

func validateTwitchChannel(ctx context.Context, channel, clientID, clientSecret string) bool {
	if clientID == "" || clientSecret == "" {
		return true
	}
	var resp struct {
		Data []any `json:"data"`
	}
	url := fmt.Sprintf("https://api.twitch.tv/helix/users?login=%s", channel)
	if err := twitchGet(ctx, url, clientID, clientSecret, &resp); err != nil {
		return true
	}
	return len(resp.Data) > 0
}

func validateTwitchCredentials(ctx context.Context, clientID, clientSecret string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://id.twitch.tv/oauth2/token", nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("client_id", clientID)
	q.Set("client_secret", clientSecret)
	q.Set("grant_type", "client_credentials")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid credentials (%d)", resp.StatusCode)
	}
	return nil
}

func validateTelegramToken(ctx context.Context, token string) (string, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("invalid token (%d)", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Ok     bool            `json:"ok"`
		Result TelegramBotInfo `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if !result.Ok {
		return "", fmt.Errorf("bot not found")
	}
	return result.Result.Username, nil
}

func waitForSetupCommand(ctx context.Context, token string, timeoutSeconds int) (int64, *int, error) {
	baseURL := fmt.Sprintf("https://api.telegram.org/bot%s", token)
	setupClient := &http.Client{Timeout: 35 * time.Second}

	offset := 0
	if req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/getUpdates?offset=0", baseURL), nil); err == nil {
		if resp, err := setupClient.Do(req); err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var updates TelegramResponse
			if json.Unmarshal(body, &updates) == nil {
				var list []TelegramUpdate
				json.Unmarshal(updates.Result, &list)
				if len(list) > 0 {
					offset = list[len(list)-1].UpdateID + 1
				}
			}
		}
	}

	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)

	for time.Now().Before(deadline) {
		url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=30", baseURL, offset)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return 0, nil, err
		}

		resp, err := setupClient.Do(req)
		if err != nil {
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var updates TelegramResponse
		if json.Unmarshal(body, &updates) != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		var list []TelegramUpdate
		json.Unmarshal(updates.Result, &list)

		for _, update := range list {
			offset = update.UpdateID + 1
			if update.Message == nil || update.Message.Text == "" {
				continue
			}
			text := strings.TrimSpace(strings.ToUpper(update.Message.Text))
			if text != "SETUP" && text != "/SETUP" {
				continue
			}

			msg := update.Message
			chatInfo := fmt.Sprintf("Chat ID: %d", msg.Chat.ID)
			if msg.Chat.Title != "" {
				chatInfo = fmt.Sprintf("%s (%s)", msg.Chat.Title, chatInfo)
			} else if msg.Chat.Username != "" {
				chatInfo = fmt.Sprintf("@%s (%d)", msg.Chat.Username, msg.Chat.ID)
			}
			fmt.Printf("\nReceived SETUP from: %s\n", chatInfo)
			if msg.MessageThreadID != nil {
				fmt.Printf("Thread ID: %d\n", *msg.MessageThreadID)
			}
			return msg.Chat.ID, msg.MessageThreadID, nil
		}

		time.Sleep(1 * time.Second)
	}

	return 0, nil, fmt.Errorf("timeout waiting for SETUP command")
}

func checkBotPermissions(ctx context.Context, token string, chatID int64) error {
	botID := getBotUserID(ctx, token)

	payload, _ := json.Marshal(map[string]any{
		"chat_id": chatID,
		"user_id": botID,
	})

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getChatMember", token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Ok     bool `json:"ok"`
		Result struct {
			Status          string `json:"status"`
			CanPostMessages *bool  `json:"can_post_messages"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if !result.Ok {
		return fmt.Errorf("failed to get bot permissions")
	}
	if result.Result.Status != "administrator" && result.Result.Status != "creator" {
		if result.Result.CanPostMessages == nil || !*result.Result.CanPostMessages {
			return fmt.Errorf("bot needs permission to send messages")
		}
	}
	return nil
}

func getBotUserID(ctx context.Context, token string) int64 {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Ok     bool `json:"ok"`
		Result struct {
			ID int64 `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}
	return result.Result.ID
}

func waitForPermissionsFix(ctx context.Context, token string, chatID int64, timeoutSeconds int) error {
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		if checkBotPermissions(ctx, token, chatID) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for permissions fix")
}
