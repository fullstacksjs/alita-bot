package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/utils/logredact"
)

func TestErrorNotifierSendsRedactedErrorsAndFlushes(t *testing.T) {
	var mu sync.Mutex
	var messages []string
	notifier := newErrorNotifier(func(_ context.Context, message string) error {
		mu.Lock()
		messages = append(messages, message)
		mu.Unlock()
		return nil
	})
	defer notifier.Stop()

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logredact.Install(logger)
	logger.AddHook(notifier)
	logger.Info("ordinary message")
	logger.WithField("error", "Authorization: Bearer abcdefgh12345").Error("TypeSafe AI analysis failed")
	notifier.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(messages) != 1 {
		t.Fatalf("got %d notifications, want one error", len(messages))
	}
	if !strings.Contains(messages[0], "TypeSafe AI analysis failed") {
		t.Fatalf("notification missing error: %q", messages[0])
	}
	if strings.Contains(messages[0], "abcdefgh12345") || !strings.Contains(messages[0], logredact.Placeholder) {
		t.Fatalf("notification was not redacted: %q", messages[0])
	}
}

func TestLimitNotificationPreservesUTF8AndTelegramLimit(t *testing.T) {
	message := limitNotification(strings.Repeat("é", 3000))
	if len(message) > errorNotificationLimit || !strings.HasSuffix(message, "... (truncated)") {
		t.Fatalf("notification size = %d, want truncated to %d bytes", len(message), errorNotificationLimit)
	}
	if !utf8.ValidString(message) {
		t.Fatal("notification has invalid UTF-8")
	}
}
