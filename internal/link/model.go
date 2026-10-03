package link

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const ProtocolVersion = 1

var Version = "dev"

type FileRef struct {
	Object string `json:"object"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Message struct {
	ID           string    `json:"id"`
	Protocol     int       `json:"protocol"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	FromName     string    `json:"from_name"`
	Session      string    `json:"session,omitempty"`
	ToSession    string    `json:"to_session,omitempty"`
	Conversation string    `json:"conversation,omitempty"`
	Kind         string    `json:"kind"`
	Text         string    `json:"text,omitempty"`
	ReplyTo      string    `json:"reply_to,omitempty"`
	File         *FileRef  `json:"file,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
type Record struct {
	Seq        int64   `json:"seq"`
	Message    Message `json:"message"`
	Direction  string  `json:"direction"`
	Status     string  `json:"status"`
	Owner      string  `json:"owner,omitempty"`
	LeaseUntil int64   `json:"lease_until,omitempty"`
	Result     string  `json:"result,omitempty"`
}
type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Protocol int       `json:"protocol"`
	SeenAt   time.Time `json:"seen_at"`
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func ValidLabel(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func (m Message) Validate() error {
	if m.Protocol != ProtocolVersion {
		return errors.New("unsupported message protocol")
	}
	if !ValidLabel(m.ID) || !ValidLabel(m.From) || !ValidLabel(m.To) || !ValidLabel(m.FromName) {
		return errors.New("invalid message identity")
	}
	for _, s := range []string{m.Session, m.ToSession, m.Conversation} {
		if s != "" && !ValidLabel(s) {
			return errors.New("invalid session/conversation")
		}
	}
	if len(m.Text) > 64*1024 {
		return errors.New("message text exceeds 64 KiB")
	}
	switch m.Kind {
	case "text", "task", "progress", "result":
		if strings.TrimSpace(m.Text) == "" {
			return errors.New("text is required")
		}
	case "file":
		if m.File == nil || m.File.Size < 0 || len(m.File.SHA256) != 64 {
			return errors.New("invalid file metadata")
		}
	case "receipt":
		if !ValidLabel(m.ReplyTo) {
			return errors.New("invalid receipt")
		}
	default:
		return errors.New("unknown message kind")
	}
	return nil
}
