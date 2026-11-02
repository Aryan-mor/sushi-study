package tg

import (
	"encoding/json"
	"testing"
)

func TestVideoFileFromVideo(t *testing.T) {
	t.Parallel()
	m := &Message{Video: &Video{FileUniqueID: "AgAD1", FileName: "a.mp4", MimeType: "video/mp4", Duration: 12, FileSize: 99}}
	got, ok := m.VideoFile()
	if !ok || got.UniqueID != "AgAD1" || got.DurationS != 12 || got.SizeBytes != 99 {
		t.Fatalf("video: %+v ok=%v", got, ok)
	}
}

func TestVideoFileFromVideoDocument(t *testing.T) {
	t.Parallel()
	m := &Message{Document: &Document{FileUniqueID: "AgAD2", FileName: "b.mkv", MimeType: "video/x-matroska", FileSize: 7}}
	got, ok := m.VideoFile()
	if !ok || got.UniqueID != "AgAD2" || got.FileName != "b.mkv" {
		t.Fatalf("doc video: %+v ok=%v", got, ok)
	}
}

func TestVideoFileFromOctetStreamMKV(t *testing.T) {
	t.Parallel()
	m := &Message{Document: &Document{FileUniqueID: "AgAD3", FileName: "Film.2024.1080p.mkv", MimeType: "application/octet-stream", FileSize: 9}}
	got, ok := m.VideoFile()
	if !ok || got.UniqueID != "AgAD3" || got.FileName != "Film.2024.1080p.mkv" {
		t.Fatalf("octet mkv: %+v ok=%v", got, ok)
	}
}

func TestVideoFileIgnoresNonVideoDocument(t *testing.T) {
	t.Parallel()
	m := &Message{Document: &Document{FileUniqueID: "x", MimeType: "application/pdf"}}
	if _, ok := m.VideoFile(); ok {
		t.Fatal("pdf must not count as video")
	}
}

func TestChannelOriginLegacyForward(t *testing.T) {
	t.Parallel()
	m := &Message{ForwardFromChat: &Chat{ID: -1001}, ForwardFromMessageID: 44}
	chat, msg, ok := m.ChannelOrigin()
	if !ok || chat != -1001 || msg != 44 {
		t.Fatalf("legacy: %d %d %v", chat, msg, ok)
	}
}

func TestChannelOriginAPIOrigin(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(map[string]any{
		"type": "channel", "date": 1,
		"chat":       map[string]any{"id": -1002},
		"message_id": 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := &Message{ForwardOrigin: raw}
	chat, msg, ok := m.ChannelOrigin()
	if !ok || chat != -1002 || msg != 9 {
		t.Fatalf("origin: %d %d %v", chat, msg, ok)
	}
}

func TestChannelOriginRejectsUserForward(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(map[string]any{"type": "user", "sender_user": map[string]any{"id": 1}})
	if err != nil {
		t.Fatal(err)
	}
	m := &Message{ForwardOrigin: raw}
	if _, _, ok := m.ChannelOrigin(); ok {
		t.Fatal("user forward must not look copyable")
	}
}
