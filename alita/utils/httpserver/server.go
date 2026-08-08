package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/utils/error_handling"
)

// maxRequestBodySize defines the maximum allowed request body size (10MB)
// This prevents DoS attacks where attackers send gigabytes of data to cause OOM
const maxRequestBodySize = 10 * 1024 * 1024

// Server represents the HTTP server that serves the health endpoint and, in
// webhook mode, the Telegram webhook endpoint.
type Server struct {
	mux            *http.ServeMux
	server         *http.Server
	port           int
	bot            *gotgbot.Bot
	dispatcher     *ext.Dispatcher
	secret         string
	webhookEnabled bool
	startTime      time.Time
	dispatchWG     sync.WaitGroup
}

// New creates a new unified HTTP server on the specified port
// The startTime parameter should be the application's process start time,
// used for accurate uptime reporting in health checks.
func New(port int, startTime time.Time) *Server {
	return &Server{
		mux:       http.NewServeMux(),
		port:      port,
		startTime: startTime,
	}
}

// HealthStatus represents the health status of the application. "process" is
// true once the HTTP server is serving; "sqlite" reflects a live ping against
// the embedded database.
type HealthStatus struct {
	Status string          `json:"status"`
	Checks map[string]bool `json:"checks"`
	Commit string          `json:"commit"`
	Uptime string          `json:"uptime"`
}

// checkDatabase checks if the SQLite database is reachable
func checkDatabase() bool {
	if db.DB == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sqlDB, err := db.DB.DB()
	if err != nil {
		return false
	}

	return sqlDB.PingContext(ctx) == nil
}

// RegisterHealth registers the /health endpoint
func (s *Server) RegisterHealth() {
	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		dbHealthy := checkDatabase()

		status := HealthStatus{
			Status: "healthy",
			Checks: map[string]bool{
				"process": true,
				"sqlite":  dbHealthy,
			},
			Commit: config.Commit,
			Uptime: time.Since(s.startTime).String(),
		}

		if !dbHealthy {
			status.Status = "unhealthy"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		if err := json.NewEncoder(w).Encode(status); err != nil {
			log.Errorf("[HTTPServer] Failed to encode health status: %v", err)
		}
	})

	log.Info("[HTTPServer] Registered /health endpoint")
}

// RegisterWebhook registers the webhook endpoint and configures the Telegram webhook
func (s *Server) RegisterWebhook(bot *gotgbot.Bot, dispatcher *ext.Dispatcher, secret, domain string) error {
	s.bot = bot
	s.dispatcher = dispatcher
	s.secret = secret
	s.webhookEnabled = true

	// Register the webhook handler at a static path — the secret is NOT in the URL.
	// Authentication is enforced by validateWebhook via the X-Telegram-Bot-Api-Secret-Token header.
	webhookPath := "/webhook"
	s.mux.HandleFunc(webhookPath, s.webhookHandler)

	// Set the webhook URL on Telegram — safe to log because the path is now secret-free.
	webhookURL := fmt.Sprintf("%s%s", domain, webhookPath)
	log.Infof("[HTTPServer] Setting webhook URL: %s", webhookURL)

	// Configure webhook options. Queued updates are never dropped: a restart
	// must not lose moderation actions Telegram buffered while we were down.
	webhookOpts := &gotgbot.SetWebhookOpts{
		AllowedUpdates: config.AppConfig.AllowedUpdates,
	}

	// Set secret token if configured
	if secret != "" {
		webhookOpts.SecretToken = secret
	}

	// Set the webhook with Telegram
	if _, err := bot.SetWebhook(webhookURL, webhookOpts); err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	log.Infof("[HTTPServer] Registered webhook endpoint at %s", webhookPath)
	return nil
}

// webhookHandler handles incoming webhook requests from Telegram
func (s *Server) webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Error("[HTTPServer] Invalid request method: ", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate the webhook secret BEFORE reading the body. The secret is in a
	// header available without consuming the body, so rejecting early avoids
	// buffering up to 10MB for unauthenticated requests (resource exhaustion).
	if !s.validateWebhook(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Read the request body with size limit to prevent DoS attacks
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodySize))
	if err != nil {
		log.Error("[HTTPServer] Failed to read request body: ", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer func() {
		if closeErr := r.Body.Close(); closeErr != nil {
			log.Errorf("[HTTPServer] Failed to close request body: %v", closeErr)
		}
	}()

	// Parse the update
	var update gotgbot.Update
	if err := json.Unmarshal(body, &update); err != nil {
		log.Error("[HTTPServer] Failed to parse update: ", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Process the update asynchronously.
	// NOTE: ProcessUpdate does not support context cancellation. Long-running handlers
	// will complete even if the HTTP response has already been sent. This is by design
	// as Telegram expects a quick 200 OK response while processing happens async.
	s.dispatchWG.Add(1)
	go func() {
		defer s.dispatchWG.Done()
		defer error_handling.RecoverFromPanic("ProcessUpdate", "HTTPServer")

		if err := s.dispatcher.ProcessUpdate(s.bot, &update, nil); err != nil {
			log.Error("[HTTPServer] Failed to process update: ", err)
		}
	}()

	// Send OK response to Telegram
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		log.Errorf("[HTTPServer] Failed to write response: %v", err)
	}
}

// validateWebhook validates the incoming webhook request using the secret token
func (s *Server) validateWebhook(r *http.Request) bool {
	if s.secret == "" {
		log.Error("[HTTPServer] Webhook secret is required but not configured - rejecting request")
		return false
	}

	// Get the X-Telegram-Bot-Api-Secret-Token header
	secretToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(secretToken), []byte(s.secret)) != 1 {
		log.Error("[HTTPServer] Invalid secret token")
		return false
	}

	return true
}

// Start starts the unified HTTP server
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Log the registered endpoints
	endpoints := []string{"/health"}
	if s.webhookEnabled {
		endpoints = append(endpoints, "/webhook")
	}
	log.Infof("[HTTPServer] Starting HTTP server on port %d with endpoints: %v", s.port, endpoints)

	// Use a channel to communicate startup errors
	errChan := make(chan error, 1)

	// Start the server in a goroutine
	go func() {
		defer error_handling.RecoverFromPanic("HTTPServer", "main")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Non-blocking send to prevent goroutine leak if error occurs after timeout
			select {
			case errChan <- err:
			default:
			}
			log.Errorf("[HTTPServer] Server failed: %v", err)
		}
	}()

	// Wait briefly to catch immediate startup errors (e.g., port conflicts)
	select {
	case err := <-errChan:
		return fmt.Errorf("failed to start HTTP server: %w", err)
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop() error {
	log.Info("[HTTPServer] Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.server == nil {
		log.Warn("[HTTPServer] Server was never started")
	} else if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}

	dispatchesDone := make(chan struct{})
	go func() {
		s.dispatchWG.Wait()
		close(dispatchesDone)
	}()
	select {
	case <-dispatchesDone:
	case <-ctx.Done():
		return fmt.Errorf("waiting for webhook dispatches: %w", ctx.Err())
	}

	log.Info("[HTTPServer] Server stopped gracefully")
	return nil
}
