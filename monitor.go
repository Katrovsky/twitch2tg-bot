package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func retryWithBackoff(ctx context.Context, operation func() error, operationName string) error {
	delays := []int{1, 3, 5, 10, 15, 30, 45, 60}

	for _, delay := range delays {
		err := operation()
		if err == nil {
			return nil
		}
		slog.Warn("operation failed, retrying", "name", operationName, "error", err, "next_in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(delay) * time.Second):
		}
	}

	for {
		err := operation()
		if err == nil {
			slog.Info("operation recovered", "name", operationName)
			return nil
		}
		slog.Warn("operation still failing", "name", operationName, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(60 * time.Second):
		}
	}
}

func monitorLoop(ctx context.Context, cfg *Config) {
	slog.Info("monitor started",
		"channel", cfg.Twitch.Channel,
		"check_interval", cfg.CheckInterval,
		"update_interval", cfg.UpdateInterval,
	)

	loc := getLocalization(cfg.Language)
	var session *StreamSession
	checksPerUpdate := (cfg.UpdateInterval * 60) / cfg.CheckInterval

	broadcasterID, err := getBroadcasterID(ctx, cfg.Twitch.Channel, cfg.Twitch.ClientID, cfg.Twitch.ClientSecret)
	if err != nil {
		slog.Error("failed to get broadcaster ID at startup", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("monitor stopped")
			return
		default:
		}

		var info *StreamInfo

		if fileExists("simulate_end") {
			slog.Info("simulate_end trigger detected")
			os.Remove("simulate_end")
		} else {
			var err error
			info, err = getStreamInfo(ctx, cfg.Twitch.Channel, cfg.Twitch.ClientID, cfg.Twitch.ClientSecret, cfg.Language)
			if err != nil {
				slog.Error("stream status check failed", "error", err)
				sleep(ctx, time.Duration(cfg.CheckInterval)*time.Second)
				continue
			}
		}

		switch {
		case info != nil && session == nil:
			slog.Info("stream started", "channel", cfg.Twitch.Channel, "viewers", info.Viewers, "game", info.Game)

			if broadcasterID == "" {
				broadcasterID, err = getBroadcasterID(ctx, cfg.Twitch.Channel, cfg.Twitch.ClientID, cfg.Twitch.ClientSecret)
				if err != nil {
					slog.Error("failed to get broadcaster ID", "error", err)
					sleep(ctx, time.Duration(cfg.CheckInterval)*time.Second)
					continue
				}
			}

			thumbnailURL := getThumbnailURL(cfg.Twitch.Channel)
			message := formatMessage(info, nil, 0, nil, loc)
			dataPoint := ViewerDataPoint{Timestamp: time.Now(), Count: info.Viewers}

			var messageID int
			retryWithBackoff(ctx, func() error {
				var sendErr error
				messageID, sendErr = sendPhotoMessage(
					ctx,
					cfg.Telegram.BotToken, *cfg.Telegram.ChatID, cfg.Telegram.ThreadID,
					thumbnailURL, message, info.URL, loc.ButtonText,
				)
				return sendErr
			}, "send start notification")

			if messageID != 0 {
				slog.Info("start notification sent")
				session = &StreamSession{
					MessageID:     messageID,
					StartTime:     time.Now(),
					Game:          info.Game,
					Title:         info.Title,
					Tags:          info.Tags,
					BroadcasterID: broadcasterID,
					ViewerHistory: []ViewerDataPoint{dataPoint},
				}
			}

		case info != nil && session != nil:
			session.ViewerHistory = append(session.ViewerHistory, ViewerDataPoint{
				Timestamp: time.Now(), Count: info.Viewers,
			})
			session.UpdateCounter++
			gameChanged := info.Game != session.Game && session.Game != ""

			if session.UpdateCounter >= checksPerUpdate || gameChanged {
				if gameChanged {
					slog.Info("game changed", "from", session.Game, "to", info.Game)
				}
				slog.Info("updating stream info", "viewers", info.Viewers, "uptime", info.Uptime)

				avgViewers := calculateAverage(session.ViewerHistory)
				thumbnailURL := getThumbnailURL(cfg.Twitch.Channel)
				clips, _ := getRecentClips(ctx, session.BroadcasterID, cfg.Twitch.ClientID, cfg.Twitch.ClientSecret, session.StartTime)
				message := formatMessage(info, session.ViewerHistory, avgViewers, clips, loc)

				retryWithBackoff(ctx, func() error {
					return editPhotoMessage(
						ctx,
						cfg.Telegram.BotToken, *cfg.Telegram.ChatID, session.MessageID,
						thumbnailURL, message, info.URL, loc.ButtonText,
					)
				}, "update stream info")

				slog.Info("stream info updated")
				session.UpdateCounter = 0
				session.Game = info.Game
				session.Title = info.Title
				session.Tags = info.Tags
			}

		case info == nil && session != nil:
			slog.Info("stream ended", "channel", cfg.Twitch.Channel)

			duration := time.Since(session.StartTime)
			durationStr := formatDuration(duration, cfg.Language)
			avgViewers := calculateAverage(session.ViewerHistory)
			maxViewers := getMaxViewers(session.ViewerHistory)

			slog.Info("stream stats",
				"duration", durationStr,
				"avg_viewers", avgViewers,
				"max_viewers", maxViewers,
			)

			clips, _ := getRecentClips(ctx, session.BroadcasterID, cfg.Twitch.ClientID, cfg.Twitch.ClientSecret, session.StartTime)
			message := formatEndMessage(cfg.Twitch.Channel, durationStr, avgViewers, maxViewers, session.Game, session.Title, session.Tags, clips, loc)
			streamURL := fmt.Sprintf("https://twitch.tv/%s", cfg.Twitch.Channel)

			retryWithBackoff(ctx, func() error {
				return editMessageCaption(
					ctx,
					cfg.Telegram.BotToken, *cfg.Telegram.ChatID, session.MessageID,
					message, streamURL, loc.ButtonText,
				)
			}, "send end notification")

			slog.Info("end notification sent")
			session = nil
		}

		sleep(ctx, time.Duration(cfg.CheckInterval)*time.Second)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
