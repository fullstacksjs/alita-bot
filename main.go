package main

import (
	"embed"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/utils/constants"

	"github.com/divkix/Alita_Robot/alita"
	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/modules"

	"github.com/divkix/Alita_Robot/alita/utils/error_handling"
	"github.com/divkix/Alita_Robot/alita/utils/errors"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
	"github.com/divkix/Alita_Robot/alita/utils/helpers"
	"github.com/divkix/Alita_Robot/alita/utils/httpserver"
	"github.com/divkix/Alita_Robot/alita/utils/shutdown"
)

//go:embed locales
var Locales embed.FS

// main initializes and starts the Alita Robot Telegram bot.
// It opens the database, loads all modules, serves the health endpoint, starts
// either polling or webhook delivery, and handles graceful shutdown.
func main() {
	// Capture process start time for accurate uptime reporting in health checks.
	// This must be captured before any initialization work begins.
	appStartTime := time.Now()

	// Health check mode for Docker healthcheck (distroless images have no curl/wget)
	if len(os.Args) > 1 && (os.Args[1] == "--health" || os.Args[1] == "-health") {
		healthPort := healthCheckPort()
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", healthPort))
		if err != nil {
			os.Exit(1)
		}
		_ = resp.Body.Close() // Ignore close error since we're exiting immediately
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Version check - print the build identity and exit without requiring services.
	// Local builds report "dev"; release builds report the injected short commit SHA.
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version" || os.Args[1] == "-v") {
		fmt.Println(config.Commit)
		os.Exit(0)
	}

	// Setup panic recovery for main goroutine
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[Main] Panic recovered: %v", r)
			os.Exit(1)
		}
	}()

	log.Infof("[Main] Starting Alita (build %s)", config.Commit)

	// Initialize the process-local locale maps (English only).
	if err := i18n.GetManager().Initialize(&Locales, "locales"); err != nil {
		log.Fatalf("Failed to initialize locale manager: %v", err)
	}
	log.Info("Locale manager initialized")

	// Create bot with a shared, connection-pooling HTTP transport.
	// The transport must be a pointer so pooling survives the by-value copy of
	// the http.Client inside BaseBotClient.
	b, err := gotgbot.NewBot(config.AppConfig.BotToken, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{
				Transport: newBotAPITransport(),
				Timeout:   constants.LongTimeout,
			},
			UseTestEnvironment: false,
			DefaultRequestOpts: &gotgbot.RequestOpts{
				Timeout: constants.LongTimeout,
				APIURL:  gotgbot.DefaultAPIURL,
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create new bot: %v", err)
	}

	// Retrieve bot identity early for logging and downstream components that reference username
	botUsername := resolveBotUsername(b)

	// some initial checks before running bot
	if err := alita.InitialChecks(b); err != nil {
		log.Fatalf("Initial checks failed: %v", err)
	}

	dispatcher := newConfiguredDispatcher()

	// Setup graceful shutdown
	shutdownManager := shutdown.NewManager()

	shutdownManager.RegisterHandler(func() error {
		log.Info("[Shutdown] Closing database connections...")
		return closeDBConnections()
	})

	// DB-using workers are registered after closeDBConnections so LIFO stops
	// them before the pool is closed.
	shutdownManager.RegisterHandler(func() error {
		log.Info("[Shutdown] Stopping anti-raid expiry poller...")
		modules.StopAntiRaidExpiryPoller()
		return nil
	})

	// HTTP server for the health endpoint and, in webhook mode, /webhook.
	httpServer := httpserver.New(config.AppConfig.HTTPPort, appStartTime)
	httpServer.RegisterHealth()

	// Check if we should use webhooks or polling
	if config.AppConfig.UseWebhooks {
		// Register webhook endpoint on the HTTP server
		if err := httpServer.RegisterWebhook(b, dispatcher, config.AppConfig.WebhookSecret, config.AppConfig.WebhookDomain); err != nil {
			log.Fatalf("[HTTPServer] Failed to register webhook: %v", err)
		}

		postInit(b, dispatcher, botUsername, "webhook")

		if err := httpServer.Start(); err != nil {
			log.Fatalf("[HTTPServer] Failed to start HTTP server: %v", err)
		}

		log.Infof("[HTTPServer] HTTP server started on port %d (health, webhook)", config.AppConfig.HTTPPort)

		// Register HTTP server shutdown handler
		shutdownManager.RegisterHandler(func() error {
			log.Info("[Shutdown] Stopping HTTP server...")
			return httpServer.Stop()
		})

		go shutdownManager.WaitForShutdown()

		// Wait for shutdown signal (blocking)
		select {}
	} else {
		// Use polling mode (default)

		// Start the HTTP server (health only in polling mode)
		if err := httpServer.Start(); err != nil {
			log.Fatalf("[HTTPServer] Failed to start HTTP server: %v", err)
		}

		log.Infof("[HTTPServer] HTTP server started on port %d (health)", config.AppConfig.HTTPPort)

		// Register HTTP server shutdown handler
		shutdownManager.RegisterHandler(func() error {
			log.Info("[Shutdown] Stopping HTTP server...")
			return httpServer.Stop()
		})

		updater := ext.NewUpdater(dispatcher, nil) // create updater with dispatcher

		if _, err = b.DeleteWebhook(nil); err != nil {
			log.Fatalf("[Polling] Failed to remove webhook: %v", err)
		}
		log.Info("[Polling] Removed Webhook!")

		postInit(b, dispatcher, botUsername, "polling")

		// Start polling. Queued updates are never dropped: a restart must not
		// lose moderation actions Telegram buffered while we were down.
		err = updater.StartPolling(b,
			&ext.PollingOpts{
				GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
					AllowedUpdates: config.AppConfig.AllowedUpdates,
				},
			},
		)
		if err != nil {
			log.Fatalf("[Polling] Failed to start polling: %v", err)
		}
		log.Info("[Polling] Started Polling...!")

		// Register handler to stop the updater on shutdown
		shutdownManager.RegisterHandler(func() error {
			log.Info("[Polling] Stopping updater...")
			err := updater.Stop()
			if err != nil {
				log.Errorf("[Polling] Error stopping updater: %v", err)
				return err
			}
			log.Info("[Polling] Updater stopped successfully")
			return nil
		})

		go shutdownManager.WaitForShutdown()

		// Idle, to keep updates coming in, and avoid bot stopping.
		updater.Idle()
	}
}

func healthCheckPort() int {
	for _, name := range []string{"HTTP_PORT", "PORT"} {
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		port, err := strconv.Atoi(value)
		if err == nil && port > 0 && port <= 65535 {
			return port
		}
		break
	}
	if config.AppConfig != nil && config.AppConfig.HTTPPort > 0 {
		return config.AppConfig.HTTPPort
	}
	return constants.DefaultHTTPPort
}

func newBotAPITransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:          constants.MaxIdleConns,
		MaxIdleConnsPerHost:   constants.MaxIdleConnsPerHost,
		MaxConnsPerHost:       constants.MaxIdleConnsPerHost + constants.MaxIdleConnsExtraBuffer,
		IdleConnTimeout:       constants.VeryLongTimeout,
		DisableCompression:    false,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     false,
		TLSHandshakeTimeout:   constants.DefaultTimeout,
		ResponseHeaderTimeout: constants.DefaultTimeout,
		ExpectContinueTimeout: constants.ShortTimeout,
	}
}

func resolveBotUsername(b *gotgbot.Bot) string {
	if me, errMe := b.GetMe(nil); errMe == nil && me != nil {
		if me.Username == "" {
			log.Warn("[Main] Bot username is empty after GetMe; deep links may not work until resolved")
		}
		return me.Username
	} else if errMe != nil {
		log.Warnf("[Main] GetMe failed during bootstrap: %v", errMe)
	}
	return ""
}

func newConfiguredDispatcher() *ext.Dispatcher {
	return ext.NewDispatcher(&ext.DispatcherOpts{
		Error:       dispatcherErrorHandler,
		MaxRoutines: constants.DispatcherMaxRoutines,
	})
}

func dispatcherErrorHandler(_ *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
	defer error_handling.RecoverFromPanic("DispatcherErrorHandler", "Main")

	logFields := log.Fields{
		"update_id": func() int64 {
			if ctx != nil && ctx.UpdateId != 0 {
				return ctx.UpdateId
			}
			return -1
		}(),
		"error_type": fmt.Sprintf("%T", err),
	}

	if wrappedErr, ok := err.(*errors.WrappedError); ok {
		logFields["file"] = wrappedErr.File
		logFields["line"] = wrappedErr.Line
		logFields["function"] = wrappedErr.Function
	}

	if helpers.IsExpectedTelegramError(err) {
		log.WithFields(logFields).Warnf("Expected Telegram API error: %v", err)
		return ext.DispatcherActionNoop
	}

	log.WithFields(logFields).Errorf("Handler error occurred: %v", err)
	return ext.DispatcherActionNoop
}

// postInit runs shared initialization before either update transport starts.
// It loads modules, sets bot commands, and sends the startup notification.
func postInit(b *gotgbot.Bot, d *ext.Dispatcher, username string, mode string) {
	alita.LoadModules(d)
	log.Infof("[Modules] Loaded modules: %s", alita.ListModules())

	// Set Commands of Bot
	tr := i18n.English()
	startDesc, _ := tr.GetString("main_bot_command_start")
	helpDesc, _ := tr.GetString("main_bot_command_help")
	_, err := b.SetMyCommands(
		[]gotgbot.BotCommand{
			{Command: "start", Description: startDesc},
			{Command: "help", Description: helpDesc},
		},
		&gotgbot.SetMyCommandsOpts{
			Scope:        gotgbot.BotCommandScopeAllPrivateChats{},
			LanguageCode: "en",
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Info("Custom bot commands set for private chats")

	sendStartupNotice(b, mode)

	if username == "" {
		log.Infof("[Bot] Bot has been started in %s mode...", mode)
	} else {
		log.Infof("[Bot] %s has been started in %s mode...", username, mode)
	}
}

// sendStartupNotice posts the startup summary to the dump chat. It is a no-op
// when MESSAGE_DUMP is not configured.
func sendStartupNotice(b *gotgbot.Bot, mode string) {
	if config.AppConfig.MessageDump == 0 {
		log.Info("[Bot] MESSAGE_DUMP is not configured; skipping startup notice")
		return
	}

	_, err := b.SendMessage(config.AppConfig.MessageDump,
		fmt.Sprintf("<b>Started Bot!</b>\n<b>Mode:</b> %s\n<b>Loaded Modules:</b>\n%s", mode, alita.ListModules()),
		&gotgbot.SendMessageOpts{
			ParseMode: formatting.HTML,
		},
	)
	if err != nil {
		log.Errorf("[Bot] Failed to send startup message to log group: %v", err)
		log.Warn("[Bot] Continuing without log channel notifications")
	}
}

// closeDBConnections closes all database connections gracefully during shutdown.
// It returns an error if the database connections cannot be closed properly.
func closeDBConnections() error {
	err := db.Close()
	if err != nil {
		log.Errorf("[Shutdown] Failed to close database connections: %v", err)
		return fmt.Errorf("failed to close database: %w", err)
	}
	log.Info("[Shutdown] Database connections closed successfully")
	return nil
}
