package types

import (
	"encoding/base64"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

// ContentPartType identifies the kind of content a ContentPart carries.
type ContentPartType string

const (
	PartText  ContentPartType = "text"
	PartImage ContentPartType = "image"
	PartFile  ContentPartType = "file" // non-image attachment; providers emit a text note for now
)

// ContentPart is one part of a multimodal message. Binary attachments carry
// Path (re-read and base64-encoded at provider-build time) rather than inline
// bytes, so persisted session JSON stays small.
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	Path     string          `json:"path,omitempty"`
	MIME     string          `json:"mime,omitempty"`
	Filename string          `json:"filename,omitempty"`
}

type Message struct {
	Role              Role          `json:"role"`
	Parts             []ContentPart `json:"parts,omitempty"`
	Thinking          string        `json:"thinking,omitempty"`
	ThinkingSignature string        `json:"thinking_signature,omitempty"`
	ToolCallID        string        `json:"tool_call_id,omitempty"`
	ToolCalls         []ToolCall    `json:"tool_calls,omitempty"`
	IsError           bool          `json:"is_error,omitempty"`
	IsPartial         bool          `json:"is_partial,omitempty"`
	// IsSummary marks a synthetic user message that summarizes earlier
	// turns after context compaction. It sits at the head of Messages so
	// downstream code can tell it apart from real user input.
	IsSummary bool      `json:"is_summary,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TextPart returns a text content part.
func TextPart(s string) ContentPart { return ContentPart{Type: PartText, Text: s} }

// NewTextMessage returns a user/assistant message carrying a single text part.
func NewTextMessage(role Role, text string) Message {
	return Message{
		Role:      role,
		Parts:     []ContentPart{TextPart(text)},
		Timestamp: time.Now(),
	}
}

// Text concatenates all text parts in order. It is the single source of truth
// for consumers that only care about text (compaction, dream, status, char
// counting, wire builders' text paths).
func (m Message) Text() string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type == PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// HasImage reports whether the message carries any image attachment.
func (m Message) HasImage() bool {
	for _, p := range m.Parts {
		if p.Type == PartImage {
			return true
		}
	}
	return false
}

// ImageFilenames returns the display names of image parts, for compaction
// notes and UI labels.
func (m Message) ImageFilenames() []string {
	var names []string
	for _, p := range m.Parts {
		if p.Type == PartImage {
			name := p.Filename
			if name == "" {
				name = filepath.Base(p.Path)
			}
			names = append(names, name)
		}
	}
	return names
}

// LoadBase64 reads the attachment bytes from Path, infers MIME if empty, and
// returns (mime, base64Data). Called by provider build steps only.
func (p ContentPart) LoadBase64() (mimeStr, b64 string, err error) {
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", "", err
	}
	mimeStr = p.MIME
	if mimeStr == "" {
		if ext := filepath.Ext(p.Path); ext != "" {
			mimeStr = mime.TypeByExtension(ext)
		}
		if mimeStr == "" {
			sniff := data
			if len(sniff) > 512 {
				sniff = sniff[:512]
			}
			mimeStr = http.DetectContentType(sniff)
		}
	}
	return mimeStr, base64.StdEncoding.EncodeToString(data), nil
}

// UnmarshalJSON accepts both the legacy {"content":"..."} form (migrated to a
// single text part) and the new {"parts":[...]} form, so persisted session
// files from before the multimodal refactor keep loading.
func (m *Message) UnmarshalJSON(data []byte) error {
	type plain Message
	var raw struct {
		plain
		Content string `json:"content"` // legacy only
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = Message(raw.plain)
	if len(m.Parts) == 0 && raw.Content != "" {
		m.Parts = []ContentPart{TextPart(raw.Content)}
	}
	return nil
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}
