package teams

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abahmed/kwatch/config"
	"github.com/abahmed/kwatch/event"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTime is the clock tests start from, so cards are reproducible.
var testTime = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

// testClock replaces the provider's clock; sleeping advances it instead of
// waiting.
type testClock struct {
	now   time.Time
	slept []time.Duration
}

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) Sleep(d time.Duration) {
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
}

// newTestTeams returns a provider for webhook with a test clock.
func newTestTeams(
	t *testing.T,
	webhook string,
	options map[string]interface{}) (*Teams, *testClock) {
	t.Helper()
	cfg := map[string]interface{}{"webhook": webhook}
	for k, v := range options {
		cfg[k] = v
	}
	c := NewTeams(cfg, &config.App{ClusterName: "prod-eu"})
	require.NotNil(t, c)

	clock := &testClock{now: testTime}
	c.now = clock.Now
	c.sleep = clock.Sleep
	return c, clock
}

type reply struct {
	status int
	header map[string]string
	body   string
}

// recorder is a test webhook that answers with the given replies in turn,
// repeating the last one, and records the requests it gets.
type recorder struct {
	*httptest.Server
	mu       sync.Mutex
	requests [][]byte
}

func newRecorder(t *testing.T, replies ...reply) *recorder {
	t.Helper()
	r := &recorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			body, _ := io.ReadAll(req.Body)
			r.mu.Lock()
			r.requests = append(r.requests, body)
			i := min(len(r.requests), len(replies)) - 1
			r.mu.Unlock()

			for k, v := range replies[i].header {
				w.Header().Set(k, v)
			}
			w.WriteHeader(replies[i].status)
			_, _ = w.Write([]byte(replies[i].body))
		}))
	t.Cleanup(r.Close)
	return r
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return ""
	}
	return string(r.requests[len(r.requests)-1])
}

func TestEmptyConfig(t *testing.T) {
	t.Setenv("TEAMS_WEBHOOK", "")

	c := NewTeams(map[string]interface{}{}, &config.App{ClusterName: "dev"})
	assert.Nil(t, c)
}

func TestName(t *testing.T) {
	c := NewTeams(
		map[string]interface{}{"webhook": "https://example.com/workflow"},
		&config.App{ClusterName: "dev"})
	require.NotNil(t, c)

	assert.Equal(t, "Microsoft Teams", c.Name())
}

func TestNewTeamsOptions(t *testing.T) {
	c := NewTeams(map[string]interface{}{
		"webhook":       "https://example.com/workflow",
		"title":         "Test Title",
		"text":          "Test Text",
		"includeLogs":   false,
		"includeEvents": false,
		"maxRetries":    5,
		"retryDelay":    2,
		"timeout":       3,
		"rateLimit":     10,
		"ratePeriod":    60,
	}, &config.App{ClusterName: "dev"})
	require.NotNil(t, c)

	assert.Equal(t, "https://example.com/workflow", c.webhook)
	assert.Equal(t, "Test Title", c.title)
	assert.Equal(t, "Test Text", c.text)
	assert.False(t, c.includeLogs)
	assert.False(t, c.includeEvents)
	assert.Equal(t, 5, c.maxRetries)
	assert.Equal(t, 2*time.Second, c.retryDelay)
	assert.Equal(t, 3*time.Second, c.client.Timeout)
	assert.Equal(t, 10, c.rateLimit)
	assert.Equal(t, time.Minute, c.ratePeriod)
}

func TestNewTeamsDefaults(t *testing.T) {
	c := NewTeams(
		map[string]interface{}{"webhook": "https://example.com/workflow"},
		&config.App{})
	require.NotNil(t, c)

	assert.True(t, c.includeLogs)
	assert.True(t, c.includeEvents)
	assert.Equal(t, defaultMaxRetries, c.maxRetries)
	assert.Equal(t, defaultRetryDelay, c.retryDelay)
	assert.Equal(t, defaultTimeout, c.client.Timeout)
	assert.Equal(t, defaultRateLimit, c.rateLimit)
	assert.Equal(t, defaultRatePeriod, c.ratePeriod)
}

func TestWebhookFromEnvironment(t *testing.T) {
	t.Setenv("TEAMS_WEBHOOK", "https://example.com/from-env")

	c := NewTeams(map[string]interface{}{}, &config.App{})
	require.NotNil(t, c)
	assert.Equal(t, "https://example.com/from-env", c.webhook)

	c = NewTeams(
		map[string]interface{}{"webhook": "https://example.com/from-config"},
		&config.App{})
	require.NotNil(t, c)
	assert.Equal(t, "https://example.com/from-config", c.webhook)
}

func TestWebhookChecks(t *testing.T) {
	t.Setenv("TEAMS_WEBHOOK", "")

	rejected := []string{
		"test",
		"h ttp://localhost/%s",
		"http://example.com/workflow",
		"https://contoso.webhook.office.com/webhookb2/abc",
		"https://outlook.office.com/webhook/abc",
	}
	for _, webhook := range rejected {
		c := NewTeams(
			map[string]interface{}{"webhook": webhook}, &config.App{})
		assert.Nil(t, c, webhook)
	}

	accepted := []string{
		"https://default1.environment.api.powerplatform.com/powerautomate/" +
			"automations/direct/workflows/abc/triggers/manual/paths/invoke",
		"https://prod-01.westus.logic.azure.com/workflows/abc",
		"http://127.0.0.1:8080/workflow",
		"http://localhost/workflow",
	}
	for _, webhook := range accepted {
		c := NewTeams(
			map[string]interface{}{"webhook": webhook}, &config.App{})
		assert.NotNil(t, c, webhook)
	}
}

func TestAcceptedCountsAsSent(t *testing.T) {
	s := newRecorder(t, reply{status: http.StatusAccepted})
	c, clock := newTestTeams(t, s.URL, nil)

	assert.NoError(t, c.SendEvent(&event.Event{PodName: "test-pod"}))
	assert.Equal(t, 1, s.count())

	assert.NoError(t, c.SendMessage("test message"))
	assert.Equal(t, 2, s.count())
	assert.Empty(t, clock.slept)
}

func TestRetries(t *testing.T) {
	retryAfter := map[string]string{"Retry-After": "7"}

	tests := []struct {
		name     string
		replies  []reply
		requests int
		errText  string
		slept    []time.Duration
	}{
		{
			name:     "ok",
			replies:  []reply{{status: http.StatusOK}},
			requests: 1,
		},
		{
			name: "schema mismatch",
			replies: []reply{{
				status: http.StatusBadRequest,
				body:   `{"error":{"code":"TriggerInputSchemaMismatch"}}`,
			}},
			requests: 1,
			errText:  "Send webhook alerts to a channel",
		},
		{
			name:     "unauthorized",
			replies:  []reply{{status: http.StatusUnauthorized}},
			requests: 1,
			errText:  `must allow "Anyone"`,
		},
		{
			name:     "not found",
			replies:  []reply{{status: http.StatusNotFound}},
			requests: 1,
			errText:  "no such workflow",
		},
		{
			name: "server error, then accepted",
			replies: []reply{
				{status: http.StatusInternalServerError},
				{status: http.StatusAccepted},
			},
			requests: 2,
		},
		{
			name: "keeps failing",
			replies: []reply{
				{status: http.StatusRequestTimeout},
				{status: http.StatusTooManyRequests},
				{status: http.StatusServiceUnavailable},
			},
			requests: 3,
			errText:  "giving up after 3 attempt(s)",
		},
		{
			name: "honours Retry-After",
			replies: []reply{
				{status: http.StatusTooManyRequests, header: retryAfter},
				{status: http.StatusAccepted},
			},
			requests: 2,
			slept:    []time.Duration{7 * time.Second},
		},
		{
			name: "Retry-After beyond the time limit",
			replies: []reply{{
				status: http.StatusTooManyRequests,
				header: map[string]string{"Retry-After": "120"},
			}},
			requests: 1,
			errText:  "giving up after 1 attempt(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newRecorder(t, tt.replies...)
			c, clock := newTestTeams(t, s.URL, nil)

			err := c.SendMessage("test message")
			assert.Equal(t, tt.requests, s.count())
			if tt.errText == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errText)
			}
			if tt.slept != nil {
				assert.Equal(t, tt.slept, clock.slept)
			}
		})
	}
}

func TestBackoffDoubles(t *testing.T) {
	s := newRecorder(t, reply{status: http.StatusBadGateway})
	c, clock := newTestTeams(t, s.URL, map[string]interface{}{
		"retryDelay": 2,
	})

	assert.Error(t, c.SendMessage("test message"))
	assert.Equal(t, 3, s.count())
	require.Len(t, clock.slept, 2)
	assert.GreaterOrEqual(t, clock.slept[0], 2*time.Second)
	assert.LessOrEqual(t, clock.slept[0], 2400*time.Millisecond)
	assert.GreaterOrEqual(t, clock.slept[1], 4*time.Second)
	assert.LessOrEqual(t, clock.slept[1], 4800*time.Millisecond)
}

func TestTimeLimit(t *testing.T) {
	s := newRecorder(t, reply{status: http.StatusBadGateway})
	c, _ := newTestTeams(t, s.URL, map[string]interface{}{
		"maxRetries": 10,
		"retryDelay": 20,
	})

	// 20 s fits in the 30 s limit, the next wait of 40 s does not.
	err := c.SendMessage("test message")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "giving up after 2 attempt(s)")
	assert.Equal(t, 2, s.count())
}

func TestNetworkErrorsAreRetried(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	webhook := s.URL + "/workflows/abc?sig=SECRET"
	s.Close()
	c, clock := newTestTeams(t, webhook, nil)

	err := c.SendMessage("test message")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "giving up after 3 attempt(s)")
	assert.Len(t, clock.slept, 2)
}

func TestWebhookNeverLogged(t *testing.T) {
	var logs bytes.Buffer
	previous := logrus.StandardLogger().Out
	logrus.SetOutput(&logs)
	t.Cleanup(func() { logrus.SetOutput(previous) })

	notFound := newRecorder(t, reply{status: http.StatusNotFound})
	slow := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
		}))
	t.Cleanup(slow.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	var errs []string
	for _, base := range []string{notFound.URL, slow.URL, closed.URL} {
		c, _ := newTestTeams(t, base+"/workflows/abc?sig=SECRET", nil)
		c.client.Timeout = 50 * time.Millisecond
		if err := c.SendMessage("test message"); err != nil {
			errs = append(errs, err.Error())
		}
	}

	require.Len(t, errs, 3)
	for _, text := range append(errs, logs.String()) {
		assert.NotContains(t, text, "SECRET")
		assert.NotContains(t, text, "sig=")
	}
	assert.Contains(t, logs.String(), "initializing Teams with a workflow "+
		"webhook on 127.0.0.1")
}

func TestRateLimit(t *testing.T) {
	s := newRecorder(t, reply{status: http.StatusAccepted})
	// Two messages a minute: a burst of two, then one every 30 s.
	c, clock := newTestTeams(t, s.URL, map[string]interface{}{
		"rateLimit":  2,
		"ratePeriod": 60,
	})

	assert.NoError(t, c.SendMessage("first"))
	assert.NoError(t, c.SendMessage("second"))
	err := c.SendMessage("third")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skipped")
	assert.Equal(t, 2, s.count())

	clock.now = clock.now.Add(30 * time.Second)
	assert.NoError(t, c.SendMessage("fourth"))
	assert.Contains(t, s.last(), "1 earlier alert was skipped")

	clock.now = clock.now.Add(30 * time.Second)
	assert.NoError(t, c.SendMessage("fifth"))
	assert.NotContains(t, s.last(), "skipped")
}

func TestSkippedCountSurvivesFailedSend(t *testing.T) {
	s := newRecorder(t,
		reply{status: http.StatusAccepted},
		reply{status: http.StatusNotFound},
		reply{status: http.StatusAccepted})
	c, clock := newTestTeams(t, s.URL, map[string]interface{}{
		"rateLimit":  1,
		"ratePeriod": 60,
	})

	assert.NoError(t, c.SendMessage("first"))
	assert.Error(t, c.SendMessage("skipped"))

	clock.now = clock.now.Add(time.Minute)
	assert.Error(t, c.SendMessage("not found"))
	assert.Contains(t, s.last(), "1 earlier alert was skipped")

	clock.now = clock.now.Add(time.Minute)
	assert.NoError(t, c.SendMessage("accepted"))
	assert.Contains(t, s.last(), "1 earlier alert was skipped")
}

func TestNilAppConfig(t *testing.T) {
	s := newRecorder(t, reply{status: http.StatusAccepted})
	c := NewTeams(map[string]interface{}{"webhook": s.URL}, nil)
	require.NotNil(t, c)

	assert.NoError(t, c.SendEvent(&event.Event{PodName: "test-pod"}))
	assert.NoError(t, c.SendMessage("test message"))
}

func TestStatusErrorTruncatesBody(t *testing.T) {
	err := statusError(http.StatusBadRequest,
		[]byte(strings.Repeat("x", 2*errorTextLimit)))

	assert.LessOrEqual(t, len(err.Error()), errorTextLimit+100)
	assert.Contains(t, err.Error(), truncationMark)
}
