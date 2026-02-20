package main

import (
	"fmt"
	"strings"
)

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func formatTags(tags []string) string {
	var hashtags []string
	for _, tag := range tags {
		if tag != "" {
			hashtags = append(hashtags, "#"+tag)
		}
	}
	return strings.Join(hashtags, " ")
}

func formatClips(clips []ClipInfo) string {
	if len(clips) == 0 {
		return ""
	}
	links := make([]string, 0, len(clips))
	for _, c := range clips {
		links = append(links, fmt.Sprintf("<a href=\"%s\">%s</a>", c.URL, escapeHTML(c.Title)))
	}
	return strings.Join(links, " · ")
}

func viewerTrend(history []ViewerDataPoint, loc Localization) string {
	if len(history) < 4 {
		return ""
	}
	mid := len(history) / 2
	var early, late float64
	for _, p := range history[:mid] {
		early += float64(p.Count)
	}
	for _, p := range history[mid:] {
		late += float64(p.Count)
	}
	early /= float64(mid)
	late /= float64(len(history) - mid)

	diff := (late - early) / early
	switch {
	case diff > 0.07:
		return loc.Growing
	case diff < -0.07:
		return loc.Dropping
	default:
		return loc.Steady
	}
}

func formatStartMessage(info *StreamInfo, loc Localization) string {
	var b strings.Builder

	line := fmt.Sprintf("<b>%s</b> • %s", escapeHTML(info.Channel), loc.StartedStreaming)
	if info.Game != "" {
		line += fmt.Sprintf(" • %s", escapeHTML(info.Game))
	}
	b.WriteString(line + "\n\n")

	if info.Title != "" {
		b.WriteString(fmt.Sprintf("<i>%s</i>", escapeHTML(info.Title)))
	}

	if tags := formatTags(info.Tags); tags != "" {
		b.WriteString("\n\n" + tags)
	}

	return b.String()
}

func formatUpdateMessage(info *StreamInfo, avgViewers int, history []ViewerDataPoint, loc Localization) string {
	var b strings.Builder

	line := fmt.Sprintf("<b>%s</b> • %s", escapeHTML(info.Channel), loc.IsLive)
	if info.Game != "" {
		line += fmt.Sprintf(" • %s", escapeHTML(info.Game))
	}
	b.WriteString(line + "\n\n")

	if info.Title != "" {
		b.WriteString(fmt.Sprintf("<i>%s</i>\n\n", escapeHTML(info.Title)))
	}

	var stats []string
	if info.Uptime != "" {
		stats = append(stats, info.Uptime)
	}
	if info.Viewers > 0 {
		v := fmt.Sprintf("%s %s", formatViewers(info.Viewers), loc.Viewers)
		if avgViewers > 0 && avgViewers != info.Viewers {
			v += fmt.Sprintf(", %s %s", formatViewers(avgViewers), loc.Avg)
		}
		if trend := viewerTrend(history, loc); trend != "" {
			v += " · " + trend
		}
		stats = append(stats, v)
	}

	b.WriteString(strings.Join(stats, " · "))

	return b.String()
}

func formatUpdateMessageWithClips(info *StreamInfo, avgViewers int, history []ViewerDataPoint, clips []ClipInfo, loc Localization) string {
	msg := formatUpdateMessage(info, avgViewers, history, loc)

	if c := formatClips(clips); c != "" {
		msg += "\n\n" + c
	}
	if tags := formatTags(info.Tags); tags != "" {
		msg += "\n\n" + tags
	}

	return msg
}

func formatEndMessage(channel, duration string, avgViewers, maxViewers int, game, title string, tags []string, clips []ClipInfo, loc Localization) string {
	var b strings.Builder

	line := fmt.Sprintf("<b>%s</b> • %s", escapeHTML(channel), loc.StreamEnded)
	if game != "" {
		line += fmt.Sprintf(" • %s", escapeHTML(game))
	}
	b.WriteString(line + "\n\n")

	if title != "" {
		b.WriteString(fmt.Sprintf("<i>%s</i>\n\n", escapeHTML(title)))
	}

	var stats []string
	if duration != "" {
		stats = append(stats, duration)
	}
	if avgViewers > 0 {
		v := fmt.Sprintf("%s %s", formatViewers(avgViewers), loc.Avg)
		if maxViewers > avgViewers {
			v += fmt.Sprintf(", %s %s", formatViewers(maxViewers), loc.Peak)
		}
		stats = append(stats, v)
	}
	if len(clips) > 0 {
		stats = append(stats, fmt.Sprintf("%d %s", len(clips), loc.Clips))
	}

	b.WriteString(strings.Join(stats, " · "))

	if c := formatClips(clips); c != "" {
		b.WriteString("\n\n" + c)
	}
	if hashtags := formatTags(tags); hashtags != "" {
		b.WriteString("\n\n" + hashtags)
	}

	return b.String()
}

func formatViewers(n int) string {
	switch {
	case n >= 1000000:
		val := float64(n) / 1000000
		if val >= 10 {
			return fmt.Sprintf("%.0fM", val)
		}
		return fmt.Sprintf("%.1fM", val)
	case n >= 10000:
		return fmt.Sprintf("%.0fK", float64(n)/1000)
	case n >= 1000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
