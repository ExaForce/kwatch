package teams

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abahmed/kwatch/config"
	"github.com/abahmed/kwatch/event"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

const (
	defaultMaxRetries = 3
	defaultRetryDelay = 5 * time.Second
	defaultTimeout    = 10 * time.Second

	// maxSendTime bounds the time spent on one message, retries included.
	// Alerts are sent from the pod handler, one provider after another.
	maxSendTime = 30 * time.Second

	// The "Send webhook alerts to a channel" template posts as the Flow bot,
	// which Microsoft limits to 25 posts per 5 minutes per connection, and
	// Teams throttles a webhook that gets more than 4 requests a second.
	defaultRateLimit  = 20
	defaultRatePeriod = 5 * time.Minute
	maxBurst          = 4

	// responseLimit bounds how much of a response is read, errorTextLimit
	// how much of it an error quotes.
	responseLimit  = 4096
	errorTextLimit = 300
)

// Teams posts alerts to a Microsoft Teams channel through a Teams Workflows
// webhook, created from the "Send webhook alerts to a channel" template.
type Teams struct {
	// webhook is the workflow URL. Its sig= parameter works like a password:
	// anyone who has it can post to the channel. It is never logged.
	webhook string

	title         string
	text          string
	includeLogs   bool
	includeEvents bool

	maxRetries int
	retryDelay time.Duration
	client     *http.Client

	limiter    *rate.Limiter
	rateLimit  int
	ratePeriod time.Duration

	// mu guards skipped: the alerts dropped by the rate limit since the last
	// delivered message, which the next card mentions.
	mu      sync.Mutex
	skipped int

	// now and sleep are replaced in tests.
	now   func() time.Time
	sleep func(time.Duration)

	// reference for general app configuration
	appCfg *config.App
}

// NewTeams returns a new Teams provider, or nil when the webhook is missing
// or cannot work. The webhook comes from the config or, failing that, from
// the TEAMS_WEBHOOK environment variable.
func NewTeams(config map[string]interface{}, appCfg *config.App) *Teams {
	webhook, _ := config["webhook"].(string)
	if len(webhook) == 0 {
		webhook = os.Getenv("TEAMS_WEBHOOK")
	}
	if len(webhook) == 0 {
		logrus.Warnf("initializing Teams with empty webhook url")
		return nil
	}

	host, err := checkWebhook(webhook)
	if err != nil {
		logrus.Errorf("initializing Teams failed: %s", err)
		return nil
	}
	logrus.Infof("initializing Teams with a workflow webhook on %s", host)

	title, _ := config["title"].(string)
	text, _ := config["text"].(string)

	rateLimit := intOption(config, "rateLimit", defaultRateLimit)
	ratePeriod := secondsOption(config, "ratePeriod", defaultRatePeriod)

	return &Teams{
		webhook:       webhook,
		title:         title,
		text:          text,
		includeLogs:   boolOption(config, "includeLogs", true),
		includeEvents: boolOption(config, "includeEvents", true),
		maxRetries:    intOption(config, "maxRetries", defaultMaxRetries),
		retryDelay:    secondsOption(config, "retryDelay", defaultRetryDelay),
		client: &http.Client{
			Timeout: secondsOption(config, "timeout", defaultTimeout),
		},
		limiter: rate.NewLimiter(
			rate.Every(ratePeriod/time.Duration(rateLimit)),
			min(rateLimit, maxBurst)),
		rateLimit:  rateLimit,
		ratePeriod: ratePeriod,
		now:        time.Now,
		sleep:      time.Sleep,
		appCfg:     appCfg,
	}
}

// Name returns name of the provider
func (t *Teams) Name() string {
	return "Microsoft Teams"
}

// SendEvent posts a card about a crashed container.
func (t *Teams) SendEvent(e *event.Event) error {
	return t.deliver(func(note string) ([]byte, error) {
		return t.eventPayload(e, note)
	})
}

// SendMessage posts a plain message, such as a volume usage alert, as a
// card.
func (t *Teams) SendMessage(msg string) error {
	return t.deliver(func(note string) ([]byte, error) {
		return t.messagePayload(msg, note)
	})
}

// deliver applies the rate limit, builds the payload and sends it. A message
// over the limit is skipped and counted, and the next card says how many
// were skipped.
func (t *Teams) deliver(build func(note string) ([]byte, error)) error {
	skipped, allowed := t.reserve()
	if !allowed {
		return fmt.Errorf(
			"skipped: more than %d messages in %s would be throttled by "+
				"Teams; %d skipped since the last one sent",
			t.rateLimit, t.ratePeriod, skipped)
	}

	payload, err := build(skippedNote(skipped))
	if err != nil {
		return err
	}
	if err := t.sendAPI(payload); err != nil {
		return err
	}

	t.mu.Lock()
	t.skipped -= skipped
	t.mu.Unlock()
	return nil
}

// reserve takes a message from the rate limit. It returns how many messages
// were skipped so far, counting this one when it is skipped too.
func (t *Teams) reserve() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.limiter.AllowN(t.now(), 1) {
		t.skipped++
		return t.skipped, false
	}
	return t.skipped, true
}

func skippedNote(skipped int) string {
	switch {
	case skipped == 1:
		return "1 earlier alert was skipped to stay under the Teams rate " +
			"limit."
	case skipped > 1:
		return fmt.Sprintf("%d earlier alerts were skipped to stay under the "+
			"Teams rate limit.", skipped)
	}
	return ""
}

// sendAPI posts a payload, retrying throttling, server and network errors
// within maxSendTime. Any 2xx reply means the workflow accepted it.
func (t *Teams) sendAPI(payload []byte) error {
	deadline := t.now().Add(maxSendTime)
	delay := t.retryDelay

	for attempt := 1; ; attempt++ {
		retry, wait, err := t.postOnce(payload)
		if err == nil {
			return nil
		}
		if !retry {
			return err
		}

		if wait == 0 {
			wait = delay + jitter(delay)
		}
		if attempt >= t.maxRetries || t.now().Add(wait).After(deadline) {
			return fmt.Errorf("giving up after %d attempt(s): %w", attempt, err)
		}
		t.sleep(wait)
		delay *= 2
	}
}

// postOnce makes one request. It reports whether a failure is worth
// retrying and how long Teams asked to wait before the next try.
func (t *Teams) postOnce(
	payload []byte) (retry bool, wait time.Duration, err error) {
	resp, err := t.client.Post(
		t.webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		return true, 0, scrubURL(err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
	resp.Body.Close()

	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		// 202 only means the workflow accepted the message. If posting it
		// fails inside the workflow, only the workflow's run history shows.
		return false, 0, nil
	case code == http.StatusRequestTimeout,
		code == http.StatusTooManyRequests,
		code >= 500:
		return true,
			retryAfter(resp.Header.Get("Retry-After"), t.now()),
			statusError(code, body)
	default:
		return false, 0, statusError(code, body)
	}
}

// statusError describes a failed reply, with a hint for the usual causes.
func statusError(code int, body []byte) error {
	text := strings.TrimSpace(string(body))

	var hint string
	switch {
	case strings.Contains(text, "TriggerInputSchemaMismatch"):
		hint = "the workflow expects another request body; create it from " +
			`the "Send webhook alerts to a channel" template`
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		hint = "the workflow refused the request; kwatch sends no token, " +
			`so the trigger must allow "Anyone"`
	case code == http.StatusNotFound:
		hint = "no such workflow: it was deleted or turned off, or its URL " +
			"changed"
	case code == http.StatusRequestEntityTooLarge:
		hint = "the message is too large"
	case code == http.StatusTooManyRequests:
		hint = "Teams is throttling the webhook"
	}

	msg := fmt.Sprintf("teams workflow returned %d", code)
	if len(hint) > 0 {
		msg += " (" + hint + ")"
	}
	if len(text) > 0 {
		msg += ": " + truncate(text, errorTextLimit)
	}
	return errors.New(msg)
}

// retryAfter reads a Retry-After header, in seconds or as an HTTP date.
func retryAfter(value string, now time.Time) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// jitter adds up to a fifth of d, so that retries from several pods don't
// line up.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return rand.N(d/5 + 1)
}

// scrubURL removes the request URL that net/http puts into its errors: the
// webhook's sig= parameter works like a password.
func scrubURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s request to teams workflow failed: %w",
			urlErr.Op, urlErr.Err)
	}
	return err
}

// checkWebhook rejects webhooks that cannot work and returns the host, which
// is safe to log.
func checkWebhook(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || len(u.Hostname()) == 0 {
		// The parse error quotes the URL, so it is left out.
		return "", errors.New("the webhook is not a valid URL")
	}
	host := strings.ToLower(u.Hostname())

	switch {
	case host == "outlook.office.com" ||
		strings.HasSuffix(host, ".webhook.office.com"):
		return "", fmt.Errorf("%s is an Office 365 connector webhook; "+
			"Microsoft retired those in May 2026, so create a Teams "+
			"Workflows webhook instead", host)
	case u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(host)):
		return "", fmt.Errorf("the webhook on %s must use https", host)
	case strings.HasSuffix(host, ".logic.azure.com"):
		logrus.Warnf("the Teams webhook is on %s: Power Automate URLs there "+
			"stopped working on 30 November 2025 (Azure Logic Apps still use "+
			"the domain); copy the current URL from the workflow's trigger",
			host)
	}
	return host, nil
}

// isLoopback allows plain http to a local test server.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (t *Teams) clusterName() string {
	if t.appCfg == nil {
		return ""
	}
	return t.appCfg.ClusterName
}

// intOption reads a positive integer option, or returns def.
func intOption(config map[string]interface{}, key string, def int) int {
	if v, ok := config[key].(int); ok && v > 0 {
		return v
	}
	return def
}

// secondsOption reads a positive number of seconds, or returns def.
func secondsOption(
	config map[string]interface{}, key string, def time.Duration) time.Duration {
	if v, ok := config[key].(int); ok && v > 0 {
		return time.Duration(v) * time.Second
	}
	return def
}

func boolOption(config map[string]interface{}, key string, def bool) bool {
	if v, ok := config[key].(bool); ok {
		return v
	}
	return def
}
