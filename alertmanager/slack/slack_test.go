package slack

import (
	"errors"
	"os"
	"testing"

	"github.com/abahmed/kwatch/config"
	"github.com/abahmed/kwatch/event"
	slackClient "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

func mockedSend(url string, msg *slackClient.WebhookMessage) error {
	return nil
}
func TestSlackEmptyConfig(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}

func TestSlackTokenWithoutChannel(t *testing.T) {
	assert := assert.New(t)

	// Token without channel should fail
	s := NewSlack(map[string]interface{}{
		"token": "xoxb-test-token",
	}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}

func TestSlackTokenWithChannel(t *testing.T) {
	assert := assert.New(t)

	// Token with channel should succeed
	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.Equal("Slack", s.Name())
	assert.NotNil(s.client)
	assert.Equal("#alerts", s.channel)
	assert.Equal("xoxb-test-token", s.token)
}

func TestSlackTokenPrecedence(t *testing.T) {
	assert := assert.New(t)

	// Token takes precedence over webhook
	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"webhook": "https://hooks.slack.com/test",
		"channel": "#alerts",
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.NotNil(s.client)
	assert.Equal("xoxb-test-token", s.token)
	assert.Empty(s.webhook)
}

func TestSlackTokenWithRoutes(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels": map[string]interface{}{
					"app": "^payments-.*",
				},
				"channels": []interface{}{"#payments-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.Len(s.routes, 1)
	assert.Equal([]string{"#payments-alerts"}, s.routes[0].channels)
}

func TestSlackRoutesInvalidRegex(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":   map[string]interface{}{"app": "[invalid"},
				"channels": []interface{}{"#payments-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}

func TestSlackRoutesEmptyLabels(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"channels": []interface{}{"#payments-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}

func TestSlackRouteSendAll(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":         map[string]interface{}{"team": "edr"},
				"channels":       []interface{}{"#edr-alerts"},
				"includeDefault": true,
			},
			map[string]interface{}{
				"sendAll":        true,
				"channels":       []interface{}{"#sre-alerts"},
				"includeDefault": true,
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	assert.Equal(
		[]string{"#sre-alerts", "#alerts"},
		s.channelsForEvent(&event.Event{}))

	assert.Equal(
		[]string{"#edr-alerts", "#alerts", "#sre-alerts"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"team": "edr",
		}}))

	var posted []string
	s.postMessage = func(
		ch string, opts ...slackClient.MsgOption) (string, string, error) {
		posted = append(posted, ch)
		return "", "", nil
	}

	assert.Nil(s.SendMessage("test"))
	assert.Equal([]string{"#sre-alerts", "#alerts"}, posted)
}

func TestSlackRouteSendAllWithLabels(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"sendAll":  true,
				"labels":   map[string]interface{}{"team": "edr"},
				"channels": []interface{}{"#sre-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}

func TestSlackRoutesEmptyChannels(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels": map[string]interface{}{"app": "payments"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}

func TestSlackRoutesIgnoredInWebhookMode(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"webhook": "testtest",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":   map[string]interface{}{"app": "payments"},
				"channels": []interface{}{"#payments-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.Empty(s.routes)
}

func TestChannelsForEvent(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels": map[string]interface{}{
					"app":  "^payments-.*",
					"tier": "backend",
				},
				"channels": []interface{}{"#payments-alerts", "#backend-oncall"},
			},
			map[string]interface{}{
				"labels":   map[string]interface{}{"tier": "backend"},
				"channels": []interface{}{"#backend-oncall", "#backend-all"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	// all labels must match (AND); both rules match here, channels
	// are deduplicated in first-seen order
	assert.Equal(
		[]string{"#payments-alerts", "#backend-oncall", "#backend-all"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"app":  "payments-api",
			"tier": "backend",
		}}))

	// only second rule matches when app label doesn't match
	assert.Equal(
		[]string{"#backend-oncall", "#backend-all"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"app":  "billing-api",
			"tier": "backend",
		}}))

	// missing label key means no match; fall back to default channel
	assert.Equal(
		[]string{"#alerts"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"app": "payments-api",
		}}))

	// no labels at all falls back to default channel
	assert.Equal(
		[]string{"#alerts"},
		s.channelsForEvent(&event.Event{}))
}

func TestChannelsForEventIncludeDefault(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":         map[string]interface{}{"team": "edr"},
				"channels":       []interface{}{"#edr-alerts"},
				"includeDefault": true,
			},
			map[string]interface{}{
				"labels":   map[string]interface{}{"team": "platform"},
				"channels": []interface{}{"#platform-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	// includeDefault adds the default channel after the route channels
	assert.Equal(
		[]string{"#edr-alerts", "#alerts"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"team": "edr",
		}}))

	// without includeDefault only the route channels are used
	assert.Equal(
		[]string{"#platform-alerts"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"team": "platform",
		}}))

	// default channel listed explicitly is not duplicated
	s.routes[0].channels = []string{"#edr-alerts", "#alerts"}
	assert.Equal(
		[]string{"#edr-alerts", "#alerts"},
		s.channelsForEvent(&event.Event{Labels: map[string]string{
			"team": "edr",
		}}))
}

func TestSendEventRouting(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":   map[string]interface{}{"team": "platform"},
				"channels": []interface{}{"#platform-alerts", "#platform-oncall"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	var posted []string
	s.postMessage = func(
		ch string, opts ...slackClient.MsgOption) (string, string, error) {
		posted = append(posted, ch)
		return "", "", nil
	}

	assert.Nil(s.SendEvent(&event.Event{
		PodName: "test-pod",
		Labels:  map[string]string{"team": "platform"},
	}))
	assert.Equal([]string{"#platform-alerts", "#platform-oncall"}, posted)

	posted = nil
	assert.Nil(s.SendEvent(&event.Event{
		PodName: "test-pod",
		Labels:  map[string]string{"team": "payments"},
	}))
	assert.Equal([]string{"#alerts"}, posted)
}

func TestSendMessageTokenDefaultChannel(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":   map[string]interface{}{"team": "platform"},
				"channels": []interface{}{"#platform-alerts"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	var posted []string
	s.postMessage = func(
		ch string, opts ...slackClient.MsgOption) (string, string, error) {
		posted = append(posted, ch)
		return "", "", nil
	}

	assert.Nil(s.SendMessage("test"))
	assert.Equal([]string{"#alerts"}, posted)
}

func TestSendEventRoutingPostError(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-test-token",
		"channel": "#alerts",
		"routes": []interface{}{
			map[string]interface{}{
				"labels":   map[string]interface{}{"team": "platform"},
				"channels": []interface{}{"#failing", "#working"},
			},
		},
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	var posted []string
	s.postMessage = func(
		ch string, opts ...slackClient.MsgOption) (string, string, error) {
		posted = append(posted, ch)
		if ch == "#failing" {
			return "", "", errors.New("channel_not_found")
		}
		return "", "", nil
	}

	err := s.SendEvent(&event.Event{
		PodName: "test-pod",
		Labels:  map[string]string{"team": "platform"},
	})
	assert.Error(err)
	assert.Contains(err.Error(), "#failing")
	// failing channel must not block the remaining channels
	assert.Equal([]string{"#failing", "#working"}, posted)
}

func TestSlack(t *testing.T) {
	assert := assert.New(t)

	configMap := map[string]interface{}{
		"webhook": "testtest",
	}
	s := NewSlack(configMap, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	assert.Equal(s.Name(), "Slack")
}

func TestSendMessage(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"webhook": "testtest",
		"channel": "test",
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	s.send = mockedSend
	assert.Nil(s.SendMessage("test"))
}

func TestSendEvent(t *testing.T) {
	assert := assert.New(t)

	s := NewSlack(map[string]interface{}{
		"webhook": "testtest",
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)

	s.send = mockedSend

	ev := event.Event{
		PodName:       "test-pod",
		ContainerName: "test-container",
		Namespace:     "default",
		Reason:        "OOMKILLED",
		Logs: "Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n" +
			"Nam quis nulla. Integer malesuada. In in enim a arcu " +
			"imperdiet malesuada. Sed vel lectus. Donec odio urna, tempus " +
			"molestie, porttitor ut, iaculis quis, sem. Phasellus rhoncus.\n",
		Events: "BackOff Back-off restarting failed container\n" +
			"event3\nevent5\nevent6-event8-event11-event12",
	}
	assert.Nil(s.SendEvent(&ev))
}

func TestSlackEnvVarToken(t *testing.T) {
	assert := assert.New(t)

	// Set environment variables
	os.Setenv("SLACK_TOKEN", "xoxb-env-token")
	os.Setenv("SLACK_CHANNEL", "#env-channel")
	defer func() {
		os.Unsetenv("SLACK_TOKEN")
		os.Unsetenv("SLACK_CHANNEL")
	}()

	// Empty config should use env vars
	s := NewSlack(map[string]interface{}{}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.Equal("xoxb-env-token", s.token)
	assert.Equal("#env-channel", s.channel)
	assert.NotNil(s.client)
}

func TestSlackEnvVarWebhook(t *testing.T) {
	assert := assert.New(t)

	// Set environment variable
	os.Setenv("SLACK_WEBHOOK", "https://hooks.slack.com/env-webhook")
	defer os.Unsetenv("SLACK_WEBHOOK")

	// Empty config should use env var
	s := NewSlack(map[string]interface{}{}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.Equal("https://hooks.slack.com/env-webhook", s.webhook)
}

func TestSlackConfigOverridesEnvVar(t *testing.T) {
	assert := assert.New(t)

	// Set environment variables
	os.Setenv("SLACK_TOKEN", "xoxb-env-token")
	os.Setenv("SLACK_CHANNEL", "#env-channel")
	defer func() {
		os.Unsetenv("SLACK_TOKEN")
		os.Unsetenv("SLACK_CHANNEL")
	}()

	// Config values should override env vars
	s := NewSlack(map[string]interface{}{
		"token":   "xoxb-config-token",
		"channel": "#config-channel",
	}, &config.App{ClusterName: "dev"})
	assert.NotNil(s)
	assert.Equal("xoxb-config-token", s.token)
	assert.Equal("#config-channel", s.channel)
}

func TestSlackEnvVarTokenWithoutChannel(t *testing.T) {
	assert := assert.New(t)

	// Set only token env var (no channel)
	os.Setenv("SLACK_TOKEN", "xoxb-env-token")
	defer os.Unsetenv("SLACK_TOKEN")

	// Should fail because channel is required with token
	s := NewSlack(map[string]interface{}{}, &config.App{ClusterName: "dev"})
	assert.Nil(s)
}
