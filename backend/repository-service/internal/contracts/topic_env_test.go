package contracts

import "testing"

func TestTopicEnv(t *testing.T) {
	t.Setenv("TEST_TOPIC", "  custom.topic ")
	if got := TopicEnv("TEST_TOPIC", "default.topic"); got != "custom.topic" {
		t.Errorf("override: got %q, want %q", got, "custom.topic")
	}
	t.Setenv("TEST_TOPIC", "   ")
	if got := TopicEnv("TEST_TOPIC", "default.topic"); got != "default.topic" {
		t.Errorf("blank: got %q, want %q", got, "default.topic")
	}
	if got := TopicEnv("TEST_TOPIC_UNSET", "default.topic"); got != "default.topic" {
		t.Errorf("unset: got %q, want %q", got, "default.topic")
	}
}
