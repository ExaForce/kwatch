package slack

import (
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
