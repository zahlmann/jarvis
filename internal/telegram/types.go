package telegram

import (
	"encoding/json"
	"fmt"
)

type MessageType string

const (
	MessageTypeText  MessageType = "text"
	MessageTypeVoice MessageType = "voice"
	MessageTypePhoto MessageType = "photo"
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
	Message          NormalizedMessage
}

type NormalizedMessage interface {
	messageType() MessageType
}

type TextMessage struct {
	Text string
}

func (TextMessage) messageType() MessageType {
	return MessageTypeText
}

type VoiceMessage struct {
	FileID   string
	MimeType string
}

func (VoiceMessage) messageType() MessageType {
	return MessageTypeVoice
}

type PhotoMessage struct {
	FileID  string
	Caption string
}

func (PhotoMessage) messageType() MessageType {
	return MessageTypePhoto
}

func (n NormalizedUpdate) Type() MessageType {
	switch n.Message.(type) {
	case TextMessage:
		return MessageTypeText
	case VoiceMessage:
		return MessageTypeVoice
	case PhotoMessage:
		return MessageTypePhoto
	default:
		panic(fmt.Sprintf("unsupported normalized message type %T", n.Message))
	}
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
	}
	if msg.ReplyToMessage != nil {
		n.ReplyToMessageID = msg.ReplyToMessage.MessageID
	}

	switch {
	case msg.Text != "":
		n.Message = TextMessage{Text: msg.Text}
		return n, nil
	case msg.Voice != nil && msg.Voice.FileID != "":
		n.Message = VoiceMessage{
			FileID:   msg.Voice.FileID,
			MimeType: msg.Voice.MimeType,
		}
		return n, nil
	case len(msg.Photo) > 0:
		largest := msg.Photo[0]
		for _, p := range msg.Photo[1:] {
			if p.FileSize > largest.FileSize {
				largest = p
			}
		}
		caption := msg.Caption
		if caption == "" {
			caption = "what do you see in this image?"
		}
		n.Message = PhotoMessage{FileID: largest.FileID, Caption: caption}
		return n, nil
	default:
		return nil, nil
	}
}
