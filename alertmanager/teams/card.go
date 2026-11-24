package teams

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abahmed/kwatch/event"
)

const (
	// cardVersion is the Adaptive Card schema version. Microsoft's Teams
	// cards reference says Teams supports up to 1.6 on desktop, web and
	// mobile.
	cardVersion = "1.6"

	// maxPayloadBytes keeps a message under the Teams limit of about 28 KB,
	// with room for what the workflow adds when it posts the card.
	maxPayloadBytes = 24 * 1024

	defaultTeamsTitle = "⛑ kwatch detected a crash in pod"

	// truncationMark shows where text was cut to fit the size limit.
	truncationMark = "…"
)

// message is the request body Microsoft documents for Teams Workflows
// webhooks. The "Send webhook alerts to a channel" template posts each
// attachment as a card and ignores everything else.
type message struct {
	Type        string       `json:"type"`
	Attachments []attachment `json:"attachments"`
}

type attachment struct {
	ContentType string `json:"contentType"`
	// ContentURL is always null, as Microsoft's docs require.
	ContentURL *string `json:"contentUrl"`
	Content    card    `json:"content"`
}

type card struct {
	Schema  string        `json:"$schema"`
	Type    string        `json:"type"`
	Version string        `json:"version"`
	Body    []interface{} `json:"body"`
}

type textBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Wrap     bool   `json:"wrap"`
	Weight   string `json:"weight,omitempty"`
	Size     string `json:"size,omitempty"`
	FontType string `json:"fontType,omitempty"`
	IsSubtle bool   `json:"isSubtle,omitempty"`
	Spacing  string `json:"spacing,omitempty"`
}

type fact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type factSet struct {
	Type  string `json:"type"`
	Facts []fact `json:"facts"`
}

// section is a titled block of lines, such as logs. total is the number of
// lines before any were dropped to fit the size limit.
type section struct {
	name  string
	lines []string
	total int
}

// newTextBlock returns a text block that wraps: without "wrap", Teams shows
// only the first line of a block.
func newTextBlock(text string) textBlock {
	return textBlock{Type: "TextBlock", Text: text, Wrap: true}
}

// encode puts a card body into the Workflows message envelope. HTML
// escaping is off, so <, > and & count as one byte against the size limit
// instead of six.
func encode(body []interface{}) ([]byte, error) {
	msg := message{
		Type: "message",
		Attachments: []attachment{{
			ContentType: "application/vnd.microsoft.card.adaptive",
			Content: card{
				Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
				Type:    "AdaptiveCard",
				Version: cardVersion,
				Body:    body,
			},
		}},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to encode teams message: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// eventPayload builds the card for a crashed container. When the message
// would be too large, it drops the oldest log lines first, because the
// crash is at the end, and then the oldest events.
func (t *Teams) eventPayload(e *event.Event, note string) ([]byte, error) {
	var logs, events []string
	if t.includeLogs {
		logs = splitLines(e.Logs)
	}
	if t.includeEvents {
		events = splitLines(e.Events)
	}
	logsTotal, eventsTotal := len(logs), len(events)

	for {
		payload, err := encode(t.eventCard(e, note,
			section{name: "Events", lines: events, total: eventsTotal},
			section{name: "Logs", lines: logs, total: logsTotal}))
		if err != nil {
			return nil, err
		}

		over := len(payload) - maxPayloadBytes
		switch {
		case over <= 0:
			return payload, nil
		case len(logs) > 1:
			logs = dropOldest(logs, over)
		case len(events) > 1:
			events = dropOldest(events, over)
		case len(logs) == 1 && len(logs[0]) > 0:
			logs[0] = keepTail(logs[0], over)
		case len(events) == 1 && len(events[0]) > 0:
			events[0] = keepTail(events[0], over)
		default:
			// Nothing is left to trim but the alert itself.
			return payload, nil
		}
	}
}

func (t *Teams) eventCard(
	e *event.Event, note string, sections ...section) []interface{} {
	title := t.title
	if len(title) == 0 {
		title = defaultTeamsTitle
	}
	heading := newTextBlock(title)
	heading.Weight = "Bolder"
	heading.Size = "Medium"
	body := []interface{}{heading}

	if len(t.text) > 0 {
		body = append(body, newTextBlock(t.text))
	}

	var facts []fact
	for _, f := range []fact{
		{Title: "Cluster", Value: t.clusterName()},
		{Title: "Namespace", Value: e.Namespace},
		{Title: "Pod", Value: e.PodName},
		{Title: "Container", Value: e.ContainerName},
		{Title: "Reason", Value: e.Reason},
	} {
		if len(f.Value) > 0 {
			facts = append(facts,
				fact{Title: f.Title, Value: escapeMarkdown(f.Value)})
		}
	}
	if len(facts) > 0 {
		body = append(body, factSet{Type: "FactSet", Facts: facts})
	}

	for _, s := range sections {
		if s.total == 0 {
			continue
		}
		name := s.name
		if len(s.lines) < s.total {
			name = fmt.Sprintf("%s (last %d of %d lines)",
				s.name, len(s.lines), s.total)
		}
		label := newTextBlock(name)
		label.Weight = "Bolder"
		label.Spacing = "Medium"
		content := newTextBlock(preformatted(s.lines))
		content.FontType = "Monospace"
		body = append(body, label, content)
	}

	return append(body, t.footer(note, false))
}

// messagePayload builds the card for a plain message, such as the startup
// message or a volume usage alert, cutting its end if it is too large.
func (t *Teams) messagePayload(msg, note string) ([]byte, error) {
	text := formatMessage(msg)
	for {
		payload, err := encode(
			[]interface{}{newTextBlock(text), t.footer(note, true)})
		if err != nil {
			return nil, err
		}

		over := len(payload) - maxPayloadBytes
		if over <= 0 || len(text) == 0 {
			return payload, nil
		}
		text = keepHead(text, over)
	}
}

// footer names the sender, the cluster (event cards show it with the pod
// details instead) and the time, followed by any note.
func (t *Teams) footer(note string, withCluster bool) textBlock {
	parts := []string{"kwatch"}
	if cluster := t.clusterName(); withCluster && len(cluster) > 0 {
		parts = append(parts, "cluster "+escapeMarkdown(cluster))
	}
	parts = append(parts, t.now().UTC().Format(time.RFC1123))

	text := strings.Join(parts, " · ")
	if len(note) > 0 {
		text += "\n\n" + note
	}

	block := newTextBlock(text)
	block.IsSubtle = true
	block.Size = "Small"
	block.Spacing = "Medium"
	return block
}

// slackEmoji turns the Slack emoji codes in kwatch's shared messages into
// characters, which is what Teams shows.
var slackEmoji = strings.NewReplacer(":tada:", "🎉", ":red_circle:", "🔴")

// slackLink matches Slack's <url|text> link syntax.
var slackLink = regexp.MustCompile(`<(https?://[^|>\s]+)\|([^>]+)>`)

// formatMessage turns a plain message written for Slack into card text.
func formatMessage(msg string) string {
	lines := splitLines(slackEmoji.Replace(msg))
	for i, line := range lines {
		// Escape first: the Markdown link brackets must stay unescaped.
		lines[i] = slackLink.ReplaceAllString(escapeMarkdown(line), "[$2]($1)")
	}
	return strings.Join(lines, "\n\n")
}

// markdownEscaper backslash-escapes what Teams reads as Markdown in card
// text. Microsoft lists bold, italic, lists and links as the supported
// subset; inline code is escaped too.
var markdownEscaper = strings.NewReplacer(
	`\`, `\\`,
	"*", `\*`,
	"_", `\_`,
	"`", "\\`",
	"[", `\[`,
	"]", `\]`,
)

// escapeMarkdown makes one line of untrusted text, such as pod data or a
// log line, show as written instead of as Markdown.
func escapeMarkdown(line string) string {
	line = markdownEscaper.Replace(line)

	// A line that starts like a list item ("- ", "+ ", "1. ", "1) ") would
	// become one.
	rest := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(rest)]
	if strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "+ ") {
		return indent + `\` + rest
	}
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits > 0 && (strings.HasPrefix(rest[digits:], ". ") ||
		strings.HasPrefix(rest[digits:], ") ")) {
		return indent + rest[:digits] + `\` + rest[digits:]
	}
	return line
}

// preformatted renders lines for a monospace text block. Each line is
// escaped and keeps its indentation, and lines are separated by a blank
// line, because Teams breaks on a single "\n" only inside lists.
func preformatted(lines []string) string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = keepIndent(escapeMarkdown(line))
	}
	return strings.Join(out, "\n\n")
}

// keepIndent turns leading spaces and tabs into no-break spaces, which
// Markdown keeps, so stack traces stay indented.
func keepIndent(line string) string {
	rest := strings.TrimLeft(line, " \t")
	if len(rest) == len(line) {
		return line
	}
	indent := strings.ReplaceAll(line[:len(line)-len(rest)], "\t", "    ")
	return strings.Repeat(" ", len(indent)) + rest
}

// splitLines splits text into lines, without the blank lines around it.
func splitLines(s string) []string {
	s = strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(strings.TrimSpace(s)) == 0 {
		return nil
	}
	return strings.Split(s, "\n")
}

// dropOldest removes lines from the start until at least over bytes are
// gone, but always keeps the last line.
func dropOldest(lines []string, over int) []string {
	removed, i := 0, 0
	for i < len(lines)-1 && removed < over {
		removed += len(lines[i]) + len("\n\n")
		i++
	}
	return lines[i:]
}

// keepTail cuts at least over bytes, plus room for the mark, from the start
// of s, at a character boundary.
func keepTail(s string, over int) string {
	cut := over + len(truncationMark)
	if cut >= len(s) {
		return ""
	}
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return truncationMark + s[cut:]
}

// keepHead cuts at least over bytes, plus room for the mark, from the end
// of s, at a character boundary.
func keepHead(s string, over int) string {
	end := len(s) - over - len(truncationMark)
	if end <= 0 {
		return ""
	}
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + truncationMark
}

// truncate shortens s to at most limit bytes.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return keepHead(s, len(s)-limit)
}
