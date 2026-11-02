package tg

import (
	"encoding/json"
	"strings"
)

// Video is a playable video attached to a message (not a gif animation).
type Video struct {
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name"`
	MimeType     string `json:"mime_type"`
	Duration     int    `json:"duration"`
	FileSize     int64  `json:"file_size"`
}

// VideoFile is the catalog-relevant slice of a message's video or video-document.
type VideoFile struct {
	UniqueID  string
	FileName  string
	MimeType  string
	DurationS int
	SizeBytes int64
}

// VideoFile reports the playable video on this message, if any. Animations (gifs) are ignored.
func (m *Message) VideoFile() (VideoFile, bool) {
	if m == nil {
		return VideoFile{}, false
	}
	if m.Video != nil && m.Video.FileUniqueID != "" {
		return VideoFile{
			UniqueID:  m.Video.FileUniqueID,
			FileName:  m.Video.FileName,
			MimeType:  m.Video.MimeType,
			DurationS: m.Video.Duration,
			SizeBytes: m.Video.FileSize,
		}, true
	}
	return videoDocument(m.Document)
}

func videoDocument(doc *Document) (VideoFile, bool) {
	if doc == nil || doc.FileUniqueID == "" {
		return VideoFile{}, false
	}
	// Dump channels often send .mkv as application/octet-stream. The name is still a video.
	if !strings.HasPrefix(doc.MimeType, "video/") && !videoFileName(doc.FileName) {
		return VideoFile{}, false
	}
	return VideoFile{
		UniqueID:  doc.FileUniqueID,
		FileName:  doc.FileName,
		MimeType:  doc.MimeType,
		SizeBytes: doc.FileSize,
	}, true
}

func videoFileName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, ext := range []string{
		".mkv", ".mp4", ".avi", ".mov", ".webm", ".m4v", ".ts", ".wmv", ".m2ts", ".mpg", ".mpeg", ".3gp",
	} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

// ChannelOrigin is the source channel post this message was forwarded from. ok is false for a
// direct upload, a user-to-user forward, or a hidden sender — the reader cannot copy those.
func (m *Message) ChannelOrigin() (chatID, msgID int64, ok bool) {
	if m == nil {
		return 0, 0, false
	}
	if m.ForwardFromChat != nil && m.ForwardFromMessageID > 0 && m.ForwardFromChat.ID < 0 {
		return m.ForwardFromChat.ID, m.ForwardFromMessageID, true
	}
	return parseForwardOrigin(m.ForwardOrigin)
}

func parseForwardOrigin(raw json.RawMessage) (int64, int64, bool) {
	if len(raw) == 0 {
		return 0, 0, false
	}
	var o struct {
		Type      string `json:"type"`
		Chat      *Chat  `json:"chat"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &o); err != nil || o.Chat == nil || o.MessageID <= 0 || o.Chat.ID >= 0 {
		return 0, 0, false
	}
	if o.Type != "channel" && o.Type != "chat" {
		return 0, 0, false
	}
	return o.Chat.ID, o.MessageID, true
}
