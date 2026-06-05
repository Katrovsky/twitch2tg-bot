package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

type TelegramMessage struct {
	MessageID int `json:"message_id"`
}

type TelegramResponse struct {
	Ok     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
}

func sendPhotoMessage(ctx context.Context, token string, chatID int64, threadID *int, photoURL, caption, buttonURL, buttonText string) (int, error) {
	imageData, err := downloadImage(ctx, photoURL)
	if err != nil {
		return 0, fmt.Errorf("failed to download image: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	writer.WriteField("caption", caption)
	writer.WriteField("parse_mode", "HTML")

	if threadID != nil {
		writer.WriteField("message_thread_id", fmt.Sprintf("%d", *threadID))
	}
	if buttonURL != "" {
		kb, _ := json.Marshal(buildKeyboard(buttonText, buttonURL))
		writer.WriteField("reply_markup", string(kb))
	}

	part, _ := writer.CreateFormFile("photo", "thumbnail.jpg")
	part.Write(imageData)
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result TelegramResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	if !result.Ok {
		return 0, fmt.Errorf("telegram API error: %s", string(respBody))
	}

	var msg TelegramMessage
	json.Unmarshal(result.Result, &msg)
	return msg.MessageID, nil
}

func editPhotoMessage(ctx context.Context, token string, chatID int64, messageID int, photoURL, caption, buttonURL, buttonText string) error {
	imageData, err := downloadImage(ctx, photoURL)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}

	type mediaObject struct {
		Type      string `json:"type"`
		Media     string `json:"media"`
		Caption   string `json:"caption"`
		ParseMode string `json:"parse_mode"`
	}
	mediaJSON, _ := json.Marshal(mediaObject{
		Type:      "photo",
		Media:     "attach://photo",
		Caption:   caption,
		ParseMode: "HTML",
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	writer.WriteField("message_id", fmt.Sprintf("%d", messageID))
	writer.WriteField("media", string(mediaJSON))

	if buttonURL != "" {
		kb, _ := json.Marshal(buildKeyboard(buttonText, buttonURL))
		writer.WriteField("reply_markup", string(kb))
	}

	part, _ := writer.CreateFormFile("photo", "thumbnail.jpg")
	part.Write(imageData)
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageMedia", token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(respBody))
	}
	return nil
}

func editMessageCaption(ctx context.Context, token string, chatID int64, messageID int, caption, buttonURL, buttonText string) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"caption":    caption,
		"parse_mode": "HTML",
	}
	if buttonURL != "" {
		payload["reply_markup"] = buildKeyboard(buttonText, buttonURL)
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageCaption", token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(respBody))
	}
	return nil
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

	status := result.Result.Status
	if status != "administrator" && status != "creator" {
		return fmt.Errorf("bot needs administrator role (current: %s)", status)
	}
	if result.Result.CanPostMessages != nil && !*result.Result.CanPostMessages {
		return fmt.Errorf("bot needs permission to send messages")
	}
	return nil
}

func buildKeyboard(text, url string) map[string]any {
	return map[string]any{
		"inline_keyboard": [][]map[string]string{
			{{"text": text, "url": url}},
		},
	}
}
