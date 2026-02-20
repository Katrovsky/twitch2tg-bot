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

func sendPhotoMessage(token string, chatID int64, threadID *int, photoURL, caption, buttonURL, buttonText string) (int, error) {
	ctx := context.Background()
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
		keyboard := buildKeyboard(buttonText, buttonURL)
		kb, _ := json.Marshal(keyboard)
		writer.WriteField("reply_markup", string(kb))
	}

	part, _ := writer.CreateFormFile("photo", "thumbnail.jpg")
	part.Write(imageData)
	writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", token)
	req, _ := http.NewRequest("POST", url, &body)
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

func editPhotoMessage(token string, chatID int64, messageID int, photoURL, caption, buttonURL, buttonText string) error {
	ctx := context.Background()
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
	req, _ := http.NewRequest("POST", url, &body)
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

func editMessageCaption(token string, chatID int64, messageID int, caption, buttonURL, buttonText string) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"caption":    caption,
		"parse_mode": "HTML",
	}
	if buttonURL != "" {
		payload["reply_markup"] = buildKeyboard(buttonText, buttonURL)
	}

	jsonData, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageCaption", token)

	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(jsonData)))
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

func buildKeyboard(text, url string) map[string]any {
	return map[string]any{
		"inline_keyboard": [][]map[string]string{
			{{"text": text, "url": url}},
		},
	}
}
