package teams

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/abahmed/kwatch/constant"
	"github.com/abahmed/kwatch/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The golden files in testdata are the exact payloads kwatch sends, so they
// can also be posted to a test workflow with curl.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func testEvent() *event.Event {
	return &event.Event{
		PodName:       "payments-api-7d9f8b6c4-x2x9q",
		ContainerName: "api",
		Namespace:     "payments",
		Reason:        "CrashLoopBackOff",
		Events: "[2026-11-24 09:58:01 +0000 UTC] BackOff Back-off restarting " +
			"failed container api in pod payments-api-7d9f8b6c4-x2x9q\n" +
			"[2026-11-24 09:59:12 +0000 UTC] Unhealthy Liveness probe " +
			"failed: HTTP probe failed with statuscode: 500",
		Logs: "2026-11-24T09:58:00Z INFO starting payments_api v1.4.2\n" +
			"2026-11-24T09:58:01Z ERROR missing env DB_PASSWORD, see " +
			"[runbook](https://example.com/runbook)\n" +
			"panic: runtime error: invalid memory address or nil pointer " +
			"dereference\n" +
			"goroutine 1 [running]:\n" +
			"main.main()\n" +
			"\t/app/main.go:42 +0x1d\n",
	}
}

// newCardTeams returns a provider for building cards; nothing is sent.
func newCardTeams(t *testing.T, options map[string]interface{}) *Teams {
	t.Helper()
	c, _ := newTestTeams(t, "https://example.com/workflow", options)
	return c
}

func checkGolden(t *testing.T, name string, payload []byte) {
	t.Helper()
	var pretty bytes.Buffer
	require.NoError(t, json.Indent(&pretty, payload, "", "  "))
	pretty.WriteByte('\n')

	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, pretty.Bytes(), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), pretty.String())
}

// cardOf decodes a payload, checks the envelope Microsoft documents for
// Workflows webhooks and returns the card.
func cardOf(t *testing.T, payload []byte) map[string]interface{} {
	t.Helper()
	var msg map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &msg))
	assert.Equal(t, "message", msg["type"])

	attachments, ok := msg["attachments"].([]interface{})
	require.True(t, ok, "attachments must be a list")
	require.Len(t, attachments, 1)
	a := attachments[0].(map[string]interface{})
	assert.Equal(t, "application/vnd.microsoft.card.adaptive", a["contentType"])
	contentURL, present := a["contentUrl"]
	assert.True(t, present, "contentUrl must be present")
	assert.Nil(t, contentURL)

	content := a["content"].(map[string]interface{})
	assert.Equal(t, "AdaptiveCard", content["type"])
	assert.Equal(t, "1.6", content["version"])
	return content
}

// textBlocks returns the card's text blocks, checking that each one wraps.
func textBlocks(
	t *testing.T, content map[string]interface{}) []map[string]interface{} {
	t.Helper()
	var blocks []map[string]interface{}
	for _, item := range content["body"].([]interface{}) {
		block := item.(map[string]interface{})
		if block["type"] == "TextBlock" {
			assert.Equal(t, true, block["wrap"], block["text"])
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func TestEventCard(t *testing.T) {
	payload, err := newCardTeams(t, nil).eventPayload(testEvent(), "")
	require.NoError(t, err)
	checkGolden(t, "event.json", payload)
	assert.NotContains(t, string(payload), "&#")

	content := cardOf(t, payload)
	facts := content["body"].([]interface{})[1].(map[string]interface{})
	assert.Equal(t, "FactSet", facts["type"])
	var got []string
	for _, f := range facts["facts"].([]interface{}) {
		m := f.(map[string]interface{})
		got = append(got, m["title"].(string)+"="+m["value"].(string))
	}
	assert.Equal(t, []string{
		"Cluster=prod-eu",
		"Namespace=payments",
		"Pod=payments-api-7d9f8b6c4-x2x9q",
		"Container=api",
		"Reason=CrashLoopBackOff",
	}, got)

	blocks := textBlocks(t, content)
	require.Len(t, blocks, 6)
	assert.Equal(t, defaultTeamsTitle, blocks[0]["text"])
	assert.Equal(t, "Events", blocks[1]["text"])
	assert.Equal(t, "Monospace", blocks[2]["fontType"])
	assert.Equal(t, "Logs", blocks[3]["text"])
	assert.Equal(t, "Monospace", blocks[4]["fontType"])
	logs := blocks[4]["text"].(string)
	assert.Contains(t, logs, `payments\_api`)
	assert.Contains(t, logs, `\[runbook\](https://example.com/runbook)`)
	assert.Contains(t, logs, "\n\npanic: runtime error")
	assert.Contains(t, logs, "\n\n\u00a0\u00a0\u00a0\u00a0/app/main.go:42 +0x1d")
	assert.Equal(t, "kwatch · Tue, 24 Nov 2026 10:00:00 UTC", blocks[5]["text"])
}

func TestMessageCard(t *testing.T) {
	payload, err := newCardTeams(t, nil).messagePayload(
		fmt.Sprintf(constant.WelcomeMsg, "v0.10.3"), "")
	require.NoError(t, err)
	checkGolden(t, "message.json", payload)

	blocks := textBlocks(t, cardOf(t, payload))
	require.Len(t, blocks, 2)
	assert.Equal(t, "🎉 kwatch@v0.10.3 just started!", blocks[0]["text"])
	assert.Equal(t,
		"kwatch · cluster prod-eu · Tue, 24 Nov 2026 10:00:00 UTC",
		blocks[1]["text"])
}

func TestUpdateMessageLink(t *testing.T) {
	payload, err := newCardTeams(t, nil).messagePayload(
		fmt.Sprintf(constant.KwatchUpdateMsg, "v1.2.0"), "")
	require.NoError(t, err)

	blocks := textBlocks(t, cardOf(t, payload))
	assert.Equal(t, "🎉 A newer version "+
		"[v1.2.0](https://github.com/abahmed/kwatch/releases/tag/v1.2.0) "+
		"of Kwatch is available! Please update to the latest version.",
		blocks[0]["text"])
}

func TestCustomTitleAndText(t *testing.T) {
	c := newCardTeams(t, map[string]interface{}{
		"title": "**Pod crashed**",
		"text":  "Check the runbook",
	})
	payload, err := c.eventPayload(testEvent(), "")
	require.NoError(t, err)

	// The configured title and text are trusted, so their Markdown is kept.
	blocks := textBlocks(t, cardOf(t, payload))
	assert.Equal(t, "**Pod crashed**", blocks[0]["text"])
	assert.Equal(t, "Check the runbook", blocks[1]["text"])
}

func TestLeaveOutLogsAndEvents(t *testing.T) {
	c := newCardTeams(t, map[string]interface{}{
		"includeLogs":   false,
		"includeEvents": false,
	})
	payload, err := c.eventPayload(testEvent(), "")
	require.NoError(t, err)

	assert.Len(t, textBlocks(t, cardOf(t, payload)), 2)
	assert.NotContains(t, string(payload), "DB_PASSWORD")
	assert.NotContains(t, string(payload), "Liveness")
}

func TestPodEventWithoutContainer(t *testing.T) {
	e := testEvent()
	e.ContainerName = ""
	payload, err := newCardTeams(t, nil).eventPayload(e, "")
	require.NoError(t, err)

	assert.NotContains(t, string(payload), `"Container"`)
}

func TestOversizedLogsKeepNewestLines(t *testing.T) {
	var logs strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&logs, "line %04d %s\n", i, strings.Repeat("x_", 35))
	}
	e := testEvent()
	e.Logs = logs.String()

	payload, err := newCardTeams(t, nil).eventPayload(e, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(payload), maxPayloadBytes)

	blocks := textBlocks(t, cardOf(t, payload))
	require.Len(t, blocks, 6)
	assert.Regexp(t, `^Logs \(last \d+ of 5000 lines\)$`, blocks[3]["text"])
	assert.Contains(t, blocks[4]["text"], "line 4999")
	assert.NotContains(t, blocks[4]["text"], "line 0000")
}

func TestOversizedLineIsCutAtCharacterBoundary(t *testing.T) {
	e := testEvent()
	e.Logs = "start " + strings.Repeat("é€", 20000) + " end"

	payload, err := newCardTeams(t, nil).eventPayload(e, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(payload), maxPayloadBytes)
	assert.True(t, utf8.Valid(payload))

	logs := textBlocks(t, cardOf(t, payload))[4]["text"].(string)
	assert.True(t, strings.HasPrefix(logs, truncationMark))
	assert.True(t, strings.HasSuffix(logs, " end"))
}

func TestOversizedEventsAndLogs(t *testing.T) {
	e := testEvent()
	e.Logs = strings.Repeat("l", 50000)
	e.Events = strings.Repeat(strings.Repeat("e", 100)+"\n", 1000)

	payload, err := newCardTeams(t, nil).eventPayload(e, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(payload), maxPayloadBytes)
	cardOf(t, payload)
}

func TestOversizedMessageIsCut(t *testing.T) {
	payload, err := newCardTeams(t, nil).messagePayload(
		strings.Repeat("ü", 30000), "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(payload), maxPayloadBytes)
	assert.True(t, utf8.Valid(payload))

	text := textBlocks(t, cardOf(t, payload))[0]["text"].(string)
	assert.True(t, strings.HasSuffix(text, truncationMark))
}

func TestHTMLCharactersAreNotEscapedInJSON(t *testing.T) {
	e := testEvent()
	e.Logs = "<html> & </html>"

	payload, err := newCardTeams(t, nil).eventPayload(e, "")
	require.NoError(t, err)
	assert.Contains(t, string(payload), "<html> & </html>")
}

func TestEscapeMarkdown(t *testing.T) {
	tests := map[string]string{
		"plain text":        "plain text",
		"payments_api":      `payments\_api`,
		"**bold**":          `\*\*bold\*\*`,
		"[link](https://x)": `\[link\](https://x)`,
		"`code`":            "\\`code\\`",
		`C:\temp`:           `C:\\temp`,
		"- item":            `\- item`,
		"  + item":          `  \+ item`,
		"12. item":          `12\. item`,
		"3) item":           `3\) item`,
		"2026-11-24 ok":     "2026-11-24 ok",
	}
	for in, want := range tests {
		assert.Equal(t, want, escapeMarkdown(in), in)
	}
}

func TestPreformatted(t *testing.T) {
	assert.Equal(t,
		"first\n\n\u00a0\u00a0\u00a0\u00a0at second\n\n\u00a0\u00a0third",
		preformatted([]string{"first", "\tat second", "  third"}))
}

func TestSplitLines(t *testing.T) {
	assert.Nil(t, splitLines(""))
	assert.Nil(t, splitLines(" \n\n"))
	assert.Equal(t,
		[]string{"a", "  b", "", "c"},
		splitLines("\na\r\n  b\n\nc\n\n"))
}

func TestKeepHeadAndTail(t *testing.T) {
	assert.Equal(t, "abc"+truncationMark, keepHead("abcdefghij", 4))
	assert.Equal(t, truncationMark+"hij", keepTail("abcdefghij", 4))
	assert.Equal(t, "", keepHead("abc", 3))
	assert.Equal(t, "", keepTail("abc", 3))
	// Cuts move to a character boundary instead of splitting "é".
	assert.True(t, utf8.ValidString(keepHead("aéééé", 2)))
	assert.True(t, utf8.ValidString(keepTail("ééééa", 2)))
}
