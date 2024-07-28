package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

var (
	discordWebhookURL string
	slackWebhookURL   string
	httpClient        = &http.Client{Timeout: 10 * time.Second}
)

type DiscordMessage struct {
	Content string  `json:"content"`
	Embeds  []Embed `json:"embeds"`
}

type Embed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
}

type SlackMessage struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

type Attachment struct {
	Color string `json:"color"`
	Text  string `json:"text"`
}

func InitNotifications() error {
	discordWebhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	if discordWebhookURL == "" {
		return fmt.Errorf("DISCORD_WEBHOOK_URL is not set in .env file")
	}

	slackWebhookURL = os.Getenv("SLACK_WEBHOOK_URL")
	if slackWebhookURL == "" {
		return fmt.Errorf("SLACK_WEBHOOK_URL is not set in .env file")
	}

	return nil
}

func SendDiscordAlert(embed Embed) error {
	message := DiscordMessage{
		Embeds: []Embed{embed},
	}
	jsonPayload, err := json.Marshal(message)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", discordWebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func SendSlackAlert(attachment Attachment) error {
	message := SlackMessage{
		Attachments: []Attachment{attachment},
	}
	jsonPayload, err := json.Marshal(message)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", slackWebhookURL, bytes.NewBuffer(jsonPayload))
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
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func GetColorForSignal(signal string) (int, string) {
	switch signal {
	case "LONG":
		return 0x00FF00, "good" // Green
	case "SHORT":
		return 0xFF0000, "danger" // Red
	default:
		return 0x0000FF, "info" // Blue
	}
}
