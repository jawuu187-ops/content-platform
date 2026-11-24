// Package messaging is direct messages between a creator and a fan.
//
// Two rules shape it. First, a thread is a pair, not a room: one creator, one fan,
// created the first time either of them writes. Second, access is checked at open
// time *and* at send time against a live entitlement — a cancelled subscription
// stops new messages without deleting the history, so a harassment report can still
// be read.
package messaging

import (
	"errors"
	"strings"
	"time"
)

// Domain errors mapped to HTTP codes by the transport layer.
var (
	ErrInvalidInput = errors.New("invalid message request")
	ErrNotFound     = errors.New("conversation not found")
	ErrForbidden    = errors.New("not a participant")
	ErrNotEntitled  = errors.New("a live subscription or unlock is required to message this creator")
	ErrSelf         = errors.New("you cannot open a conversation with yourself")
	ErrBodyTooLong  = errors.New("message body is too long")
)

// Limits are exported so the client can show the same numbers.
const (
	MaxBodyLength = 4000
	MaxThreadSize = 50
)

// Conversation is one thread between a creator and a fan.
type Conversation struct {
	ID            string     `json:"id"`
	CreatorID     string     `json:"creator_id"`
	FanID         string     `json:"fan_id"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	// PeerName is the other participant's display name, resolved server-side so a
	// client never has to ask "which id am I?" before it can render a list.
	PeerName string `json:"peer_name"`
	// CanReply is false once the entitlement lapses. History stays readable.
	CanReply bool `json:"can_reply"`
}

// Message is one immutable line in a thread.
type Message struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	SenderID       string     `json:"sender_id"`
	Body           string     `json:"body"`
	SentAt         time.Time  `json:"sent_at"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	// Mine saves the client from comparing ids to decide which side of the thread
	// a message belongs on.
	Mine bool `json:"mine"`
}

// NormalizeBody trims a body and rejects empty or oversized messages. Control
// characters other than newline and tab are stripped: they are never content and
// they break every client that renders the text.
func NormalizeBody(raw string) (string, error) {
	if len(raw) > MaxBodyLength*4 {
		return "", ErrBodyTooLong
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", errors.New("message body is empty")
	}
	if len([]rune(cleaned)) > MaxBodyLength {
		return "", ErrBodyTooLong
	}
	return cleaned, nil
}
