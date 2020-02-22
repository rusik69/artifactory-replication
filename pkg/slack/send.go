package slack

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

func SendMessage(msg string) error {
	slackWebhook := os.Getenv("SLACK_WEBHOOK")
	channel := os.Getenv("SLACK_CHANNEL")
	user := os.Getenv("SLACK_USER")
	buildUrl := os.Getenv("BUILD_URL")
	if buildUrl != "" {
		msg = buildUrl + "\n" + msg
	}
	if slackWebhook != "" {
		log.Println("Sending slack notification...")
		type SlackRequestBody struct {
			Text    string `json:"text"`
			Channel string `json:"channel"`
			User    string `json:"username"`
		}
		slackBody, _ := json.Marshal(SlackRequestBody{Text: msg, Channel: channel, User: user})
		req, err := http.NewRequest(http.MethodPost, slackWebhook, bytes.NewBuffer(slackBody))
		if err != nil {
			return err
		}
		req.Header.Add("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		log.Println(buf.String())
		if buf.String() != "ok" {
			return errors.New("Non-ok response returned from Slack")
		}
	}
	return nil
}
