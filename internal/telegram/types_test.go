package telegram

import "testing"

func TestNormalizeUpdateText(t *testing.T) {
	u := Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 101,
			Text:      "hello",
			Chat:      Chat{ID: 42},
			From:      User{ID: 7, FirstName: "Ada"},
		},
	}
	n, err := NormalizeUpdate(u)
	if err != nil {
		t.Fatalf("NormalizeUpdate returned error: %v", err)
	}
	if n == nil || n.Type != "text" || n.Text != "hello" {
		t.Fatalf("unexpected normalized update: %#v", n)
	}
}

func TestNormalizeUpdateVoice(t *testing.T) {
	u := Update{
		UpdateID: 2,
		Message: &Message{
			MessageID: 102,
			Chat:      Chat{ID: 42},
			From:      User{ID: 8, Username: "voice_user"},
			Voice:     &Voice{FileID: "voice-file", MimeType: "audio/ogg"},
		},
	}
	n, err := NormalizeUpdate(u)
	if err != nil {
		t.Fatalf("NormalizeUpdate returned error: %v", err)
	}
	if n == nil || n.Type != "voice" || n.VoiceFileID != "voice-file" {
		t.Fatalf("unexpected normalized voice update: %#v", n)
	}
}

func TestNormalizeUpdatePhotoSelectsLargest(t *testing.T) {
	u := Update{
		UpdateID: 3,
		Message: &Message{
			MessageID: 103,
			Chat:      Chat{ID: 42},
			From:      User{ID: 9, FirstName: "Pic"},
			Caption:   "caption",
			Photo: []Photo{
				{FileID: "small", FileSize: 10},
				{FileID: "big", FileSize: 99},
			},
		},
	}
	n, err := NormalizeUpdate(u)
	if err != nil {
		t.Fatalf("NormalizeUpdate returned error: %v", err)
	}
	if n == nil || n.Type != "photo" || n.PhotoFileID != "big" {
		t.Fatalf("unexpected normalized photo update: %#v", n)
	}
}
