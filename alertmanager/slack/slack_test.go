package slack

import (
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
