package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type StreamInfo struct {
	Channel string
	URL     string
	Title   string
	Game    string
	Viewers int
	Uptime  string
	Tags    []string
}

type ClipInfo struct {
	URL   string
	Title string
}

type TwitchAuthResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type TwitchStream struct {
	UserLogin   string    `json:"user_login"`
	GameName    string    `json:"game_name"`
	Title       string    `json:"title"`
	ViewerCount int       `json:"viewer_count"`
	StartedAt   time.Time `json:"started_at"`
	Tags        []string  `json:"tags"`
}

type TwitchStreamsResponse struct {
	Data []TwitchStream `json:"data"`
}

type TwitchClip struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	ViewCount int       `json:"view_count"`
	CreatedAt time.Time `json:"created_at"`
}

type TwitchClipsResponse struct {
	Data []TwitchClip `json:"data"`
}

var (
	tokenMu           sync.Mutex
	twitchAccessToken string
	tokenExpiresAt    time.Time
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func getAccessToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if twitchAccessToken != "" && time.Now().Before(tokenExpiresAt) {
		return twitchAccessToken, nil
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://id.twitch.tv/oauth2/token", nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	q.Set("client_id", clientID)
	q.Set("client_secret", clientSecret)
	q.Set("grant_type", "client_credentials")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth failed (%d): %s", resp.StatusCode, body)
	}

	var auth TwitchAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		return "", err
	}

	twitchAccessToken = auth.AccessToken
	tokenExpiresAt = time.Now().Add(time.Duration(auth.ExpiresIn-300) * time.Second)
	return twitchAccessToken, nil
}

func twitchGet(ctx context.Context, url, clientID, clientSecret string, out any) error {
	token, err := getAccessToken(ctx, clientID, clientSecret)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Client-ID", clientID)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twitch API error (%d): %s", resp.StatusCode, body)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func getStreamInfo(ctx context.Context, channel, clientID, clientSecret, lang string) (*StreamInfo, error) {
	url := fmt.Sprintf("https://api.twitch.tv/helix/streams?user_login=%s", channel)

	var resp TwitchStreamsResponse
	if err := twitchGet(ctx, url, clientID, clientSecret, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, nil
	}

	s := resp.Data[0]
	return &StreamInfo{
		Channel: s.UserLogin,
		URL:     fmt.Sprintf("https://twitch.tv/%s", s.UserLogin),
		Title:   s.Title,
		Game:    s.GameName,
		Viewers: s.ViewerCount,
		Uptime:  formatDuration(time.Since(s.StartedAt), lang),
		Tags:    s.Tags,
	}, nil
}

func getBroadcasterID(ctx context.Context, channel, clientID, clientSecret string) (string, error) {
	url := fmt.Sprintf("https://api.twitch.tv/helix/users?login=%s", channel)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := twitchGet(ctx, url, clientID, clientSecret, &resp); err != nil {
		return "", err
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("broadcaster not found: %s", channel)
	}
	return resp.Data[0].ID, nil
}

func getRecentClips(ctx context.Context, broadcasterID, clientID, clientSecret string, since time.Time) ([]ClipInfo, error) {
	url := fmt.Sprintf(
		"https://api.twitch.tv/helix/clips?broadcaster_id=%s&started_at=%s&ended_at=%s&first=20",
		broadcasterID,
		since.UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)

	var resp TwitchClipsResponse
	if err := twitchGet(ctx, url, clientID, clientSecret, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}

	clips := make([]ClipInfo, 0, len(resp.Data))
	for _, c := range resp.Data {
		clips = append(clips, ClipInfo{URL: c.URL, Title: c.Title})
	}
	return clips, nil
}

func formatDuration(d time.Duration, lang string) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if lang == "ru" {
		if hours > 0 {
			return fmt.Sprintf("%d ч %d мин", hours, minutes)
		}
		return fmt.Sprintf("%d мин", minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%d h %d m", hours, minutes)
	}
	return fmt.Sprintf("%d m", minutes)
}

func getThumbnailURL(channel string) string {
	return fmt.Sprintf("https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-1920x1080.jpg?t=%d",
		channel, time.Now().Unix())
}

func downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download failed: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
