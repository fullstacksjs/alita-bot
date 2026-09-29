package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/utils/error_handling"
	"github.com/divkix/Alita_Robot/alita/utils/logredact"
)

const errorNotificationLimit = 4096

// errorNotifier sends error-level log entries to the configured Telegram chat.
// Ordinary error logs are queued so a slow Telegram API cannot block a bot handler.
type errorNotifier struct {
	mu      sync.Mutex
	queue   chan string
	done    chan struct{}
	stopped bool
	send    func(context.Context, string) error
}

func newErrorNotifier(send func(context.Context, string) error) *errorNotifier {
	n := &errorNotifier{
		queue: make(chan string, 100),
		done:  make(chan struct{}),
		send:  send,
	}
	go n.run()
	return n
}

func (n *errorNotifier) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel}
}

func (n *errorNotifier) Fire(entry *logrus.Entry) error {
	// The redaction hook is installed first, but scrub the formatted entry too:
	// nested fields may contain strings the redaction hook does not inspect.
	formatted, err := entry.Logger.Formatter.Format(entry)
	if err != nil {
		return err
	}
	message := limitNotification("Bot error\n" + logredact.Scrub(strings.TrimSpace(string(formatted))))
	if entry.Level == logrus.FatalLevel || entry.Level == logrus.PanicLevel {
		// logrus exits or panics after Fire returns, so the worker may never run.
		n.sendMessage(message)
		return nil
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return nil
	}
	select {
	case n.queue <- message:
	default:
		// Keep the original error in Docker logs when Telegram falls behind.
		fmt.Fprintln(os.Stderr, "[MessageDump] Error notification queue full; notification dropped")
	}
	return nil
}

func (n *errorNotifier) run() {
	defer close(n.done)
	defer error_handling.RecoverFromPanic("run", "MessageDump")
	for message := range n.queue {
		n.sendMessage(message)
	}
}

func (n *errorNotifier) sendMessage(message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.send(ctx, message); err != nil {
		// Logging through logrus here would enqueue the failure again.
		fmt.Fprintf(os.Stderr, "[MessageDump] Failed to send error notification: %s\n", logredact.Scrub(err.Error()))
	}
}

func (n *errorNotifier) Stop() {
	n.mu.Lock()
	if !n.stopped {
		n.stopped = true
		close(n.queue)
	}
	n.mu.Unlock()
	<-n.done
}

func limitNotification(message string) string {
	if len(message) <= errorNotificationLimit {
		return message
	}
	const suffix = "... (truncated)"
	limit := errorNotificationLimit - len(suffix)
	i := 0
	for i < len(message) {
		_, size := utf8.DecodeRuneInString(message[i:])
		if i+size > limit {
			break
		}
		i += size
	}
	return message[:i] + suffix
}
