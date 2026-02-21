package telegram

import (
	"encoding/json"
	"fmt"
)

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

type Message struct {
	MessageID      int64    `json:"message_id"`
	Date           int64    `json:"date"`
	Text           string   `json:"text,omitempty"`
	Caption        string   `json:"caption,omitempty"`
	Chat           Chat     `json:"chat"`
	From           User     `json:"from"`
	Voice          *Voice   `json:"voice,omitempty"`
	Photo          []Photo  `json:"photo,omitempty"`
	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Voice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type,omitempty"`
}

type Photo struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

type NormalizedUpdate struct {
	UpdateID         int64
	ChatID           int64
	MessageID        int64
	ReplyToMessageID int64
	UserID           int64
	UserName         string
	Type             string
	Text             string
	VoiceFileID      string
	VoiceMimeType    string
	PhotoFileID      string
	Caption          string
}

func ParseUpdate(body []byte) (Update, error) {
	var u Update
	if err := json.Unmarshal(body, &u); err != nil {
		return Update{}, err
	}
	return u, nil
}

func NormalizeUpdate(u Update) (*NormalizedUpdate, error) {
	if u.Message == nil {
		return nil, nil
	}
	msg := u.Message
	if msg.Chat.ID == 0 || msg.MessageID == 0 {
		return nil, fmt.Errorf("missing chat or message id")
	}

	name := msg.From.FirstName
	if msg.From.LastName != "" {
		name = name + " " + msg.From.LastName
	}
	if name == "" {
		name = msg.From.Username
	}
	if name == "" {
		name = "user"
	}

	n := &NormalizedUpdate{
		UpdateID:  u.UpdateID,
		ChatID:    msg.Chat.ID,
		MessageID: msg.MessageID,
		UserID:    msg.From.ID,
		UserName:  name,
		Caption:   msg.Caption,
	}
	if msg.ReplyToMessage != nil {
		n.ReplyToMessageID = msg.ReplyToMessage.MessageID
	}

	switch {
	case msg.Text != "":
		n.Type = "text"
		n.Text = msg.Text
		return n, nil
	case msg.Voice != nil && msg.Voice.FileID != "":
		n.Type = "voice"
		n.VoiceFileID = msg.Voice.FileID
		n.VoiceMimeType = msg.Voice.MimeType
		return n, nil
	case len(msg.Photo) > 0:
		n.Type = "photo"
		largest := msg.Photo[0]
		for _, p := range msg.Photo[1:] {
			if p.FileSize > largest.FileSize {
				largest = p
			}
		}
		n.PhotoFileID = largest.FileID
		if n.Caption == "" {
			n.Caption = "what do you see in this image?"
		}
		return n, nil
	default:
		return nil, nil
	}
}
