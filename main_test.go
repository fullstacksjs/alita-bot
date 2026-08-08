package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/utils/constants"
	alitaerrors "github.com/divkix/Alita_Robot/alita/utils/errors"
)

type mainBotCall struct {
	method string
	params map[string]any
}

type mainBotClient struct {
	calls []mainBotCall
}

func (c *mainBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	c.calls = append(c.calls, mainBotCall{method: method, params: params})
	switch method {
	case "getMe":
		return json.RawMessage(`{"id":999,"is_bot":true,"first_name":"Alita","username":"AlitaTestBot"}`), nil
	case "setMyCommands":
		return json.RawMessage(`true`), nil
	case "sendMessage":
		return json.RawMessage(`{"message_id":1,"date":1,"chat":{"id":-1001,"type":"supergroup"}}`), nil
	default:
		return nil, gotgbot.ErrInvalidTokenFormat
	}
}

func (c *mainBotClient) GetAPIURL(opts *gotgbot.RequestOpts) string {
	if opts != nil && opts.APIURL != "" {
		return strings.TrimSuffix(opts.APIURL, "/")
	}
	return "https://api.telegram.org"
}

func (c *mainBotClient) FileURL(token string, tgFilePath string, opts *gotgbot.RequestOpts) string {
	return c.GetAPIURL(opts) + "/file/bot" + token + "/" + tgFilePath
}

func TestNewBotAPITransportKeepsConnectionTuning(t *testing.T) {
	transport := newBotAPITransport()
	if transport.MaxIdleConns != constants.MaxIdleConns || transport.MaxIdleConnsPerHost != constants.MaxIdleConnsPerHost {
		t.Fatalf(
			"transport limits = (%d, %d), want (%d, %d)",
			transport.MaxIdleConns,
			transport.MaxIdleConnsPerHost,
			constants.MaxIdleConns,
			constants.MaxIdleConnsPerHost,
		)
	}
	if transport.MaxConnsPerHost <= transport.MaxIdleConnsPerHost {
		t.Fatalf("MaxConnsPerHost = %d, want more than MaxIdleConnsPerHost", transport.MaxConnsPerHost)
	}
}

func TestHealthCheckPortUsesProviderEnvironment(t *testing.T) {
	previousConfig := config.AppConfig
	config.AppConfig = &config.Config{HTTPPort: 8080}
	t.Cleanup(func() { config.AppConfig = previousConfig })

	t.Setenv("HTTP_PORT", "")
	t.Setenv("PORT", "9090")
	if got := healthCheckPort(); got != 9090 {
		t.Fatalf("healthCheckPort() = %d, want Railway PORT 9090", got)
	}

	t.Setenv("HTTP_PORT", "7070")
	if got := healthCheckPort(); got != 7070 {
		t.Fatalf("healthCheckPort() = %d, want HTTP_PORT 7070", got)
	}
}

func TestMainVersionModeReportsBuildIdentity(t *testing.T) {
	t.Run("local build reports dev", func(t *testing.T) {
		output, err := helperMainCommand(t, "--version").CombinedOutput()
		if err != nil {
			t.Fatalf("main --version exited with error: %v\n%s", err, output)
		}
		if got := strings.TrimSpace(string(output)); got != "dev" {
			t.Fatalf("main --version output = %q, want dev", got)
		}
	})

	t.Run("injected build reports the short commit SHA", func(t *testing.T) {
		cmd := helperMainCommand(t, "--version")
		cmd.Env = append(cmd.Env, "ALITA_TEST_MAIN_COMMIT=a1b2c3d")

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("main --version exited with error: %v\n%s", err, output)
		}
		if got := strings.TrimSpace(string(output)); got != "a1b2c3d" {
			t.Fatalf("main --version output = %q, want injected commit", got)
		}
	})
}

func TestMainHealthModeExitsByStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "healthy", statusCode: http.StatusOK},
		{name: "unhealthy", statusCode: http.StatusServiceUnavailable, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			t.Cleanup(server.Close)

			port := serverPort(t, server.URL)
			cmd := helperMainCommand(t, "--health")
			cmd.Env = append(cmd.Env, "HTTP_PORT="+port, "PORT=")

			output, err := cmd.CombinedOutput()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("main --health succeeded, want exit error\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("main --health exited with error: %v\n%s", err, output)
			}
		})
	}
}

func TestCloseDBConnectionsAllowsNilDatabase(t *testing.T) {
	if err := closeDBConnections(); err != nil {
		t.Fatalf("close nil database: %v", err)
	}
}

func TestPostInitSetsCommandsAndStartupMessage(t *testing.T) {
	previousConfig := config.AppConfig
	config.AppConfig.MessageDump = -100123
	t.Cleanup(func() {
		config.AppConfig = previousConfig
	})

	previousDB := db.DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	db.DB = testDB
	t.Cleanup(func() {
		db.DB = previousDB
	})

	client := &mainBotClient{}
	bot := &gotgbot.Bot{
		Token:     "999:test",
		BotClient: client,
		User: gotgbot.User{
			Id:       999,
			IsBot:    true,
			Username: "AlitaTestBot",
		},
	}
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})

	postInit(bot, dispatcher, bot.Username, "polling")

	if len(client.calls) != 2 {
		t.Fatalf("got %d bot calls, want setMyCommands and sendMessage", len(client.calls))
	}
	if client.calls[0].method != "setMyCommands" {
		t.Fatalf("first call = %s, want setMyCommands", client.calls[0].method)
	}
	if client.calls[1].method != "sendMessage" {
		t.Fatalf("second call = %s, want sendMessage", client.calls[1].method)
	}
	if got := client.calls[1].params["chat_id"]; got != int64(-100123) {
		t.Fatalf("startup message chat_id = %#v, want MessageDump", got)
	}
}

// TestSendStartupNoticeSkipsWithoutMessageDump verifies that an unconfigured
// MESSAGE_DUMP skips the dump-chat notice instead of sending to chat 0.
func TestSendStartupNoticeSkipsWithoutMessageDump(t *testing.T) {
	previousDump := config.AppConfig.MessageDump
	config.AppConfig.MessageDump = 0
	t.Cleanup(func() { config.AppConfig.MessageDump = previousDump })

	client := &mainBotClient{}
	bot := &gotgbot.Bot{
		Token:     "999:test",
		BotClient: client,
		User:      gotgbot.User{Id: 999, IsBot: true, Username: "AlitaTestBot"},
	}

	sendStartupNotice(bot, "polling")

	if len(client.calls) != 0 {
		t.Fatalf("got %d bot calls, want none when MESSAGE_DUMP is unset", len(client.calls))
	}
}

func TestResolveBotUsernameReadsGetMeResponse(t *testing.T) {
	client := &mainBotClient{}
	bot := &gotgbot.Bot{
		Token:     "999:test",
		BotClient: client,
		User:      gotgbot.User{Id: 999, IsBot: true},
	}

	if got := resolveBotUsername(bot); got != "AlitaTestBot" {
		t.Fatalf("resolveBotUsername() = %q, want AlitaTestBot", got)
	}
}

func TestNewDispatcherHandlesExpectedAndWrappedErrors(t *testing.T) {
	dispatcher := newConfiguredDispatcher()
	if dispatcher == nil {
		t.Fatal("newConfiguredDispatcher() = nil")
	}
	if dispatcher.Error == nil {
		t.Fatal("dispatcher Error handler is nil")
	}

	ctx := &ext.Context{Update: &gotgbot.Update{UpdateId: 42}}
	action := dispatcher.Error(nil, ctx, &gotgbot.TelegramError{Description: "Bad Request: message to delete not found"})
	if action != ext.DispatcherActionNoop {
		t.Fatalf("expected Telegram error action = %s, want noop", action)
	}

	action = dispatcher.Error(nil, ctx, alitaerrors.Wrap(assertErr{}, "wrapped failure"))
	if action != ext.DispatcherActionNoop {
		t.Fatalf("wrapped error action = %s, want noop", action)
	}
}

type assertErr struct{}

func (assertErr) Error() string {
	return "assert error"
}

func TestHelperMainProcess(t *testing.T) {
	if os.Getenv("ALITA_TEST_MAIN_PROCESS") != "1" {
		return
	}

	if commit := os.Getenv("ALITA_TEST_MAIN_COMMIT"); commit != "" {
		config.Commit = commit
	}
	args := []string{os.Args[0]}
	if sep := slicesIndex(os.Args, "--"); sep >= 0 && sep+1 < len(os.Args) {
		args = append(args, os.Args[sep+1:]...)
	}
	os.Args = args
	main()
}

func helperMainCommand(t *testing.T, arg string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperMainProcess$", "--", arg)
	cmd.Env = append(os.Environ(), "ALITA_TEST_MAIN_PROCESS=1")
	return cmd
}

func serverPort(t *testing.T, rawURL string) string {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, port, ok := strings.Cut(parsed.Host, ":")
	if !ok || port == "" {
		t.Fatalf("server URL has no port: %s", rawURL)
	}
	return port
}

func slicesIndex(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
