package slack

import (
	"github.com/slack-go/slack"
)

type Client struct {
	api *slack.Client
}

func NewClient(token string) *Client {
	api := slack.New(token)
	return &Client{api: api}
}

// you’ll add methods like PostMessage(), ListChannels(), etc.

func (c *Client) PostMessage(channelID, text string) error {
	_, _, err := c.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	return err
}

// maybe also methods to fetch history, subscribe, etc.
