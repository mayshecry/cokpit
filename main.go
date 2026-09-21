package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"cockpit/pkg/api"
	"cockpit/pkg/auth"
	"cockpit/pkg/db"
)

func main() {
	logger := log.New(os.Stdout, "[cockpit] ", log.LstdFlags|log.Lmsgprefix)

	dbPath := envOr("DB_PATH", "cockpit.db")
	port := envOr("PORT", "80")
	slaHours, err := strconv.Atoi(envOr("SLA_TARGET_HOURS", "24"))
	if err != nil || slaHours <= 0 {
		logger.Printf("invalid SLA_TARGET_HOURS value %q, using 24", os.Getenv("SLA_TARGET_HOURS"))
		slaHours = 24
	}
	sessionHours, err := strconv.Atoi(envOr("SESSION_HOURS", "24"))
	if err != nil || sessionHours <= 0 {
		logger.Printf("invalid SESSION_HOURS value %q, using 24", os.Getenv("SESSION_HOURS"))
		sessionHours = 24
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		logger.Fatalf("database init: %v", err)
	}
	defer conn.Close()

	store := db.New(conn)

	adminUser := envOr("ADMIN_USERNAME", "admin")
	adminPass := envOr("ADMIN_PASSWORD", "admin")
	hash, err := auth.HashPassword(adminPass)
	if err != nil {
		logger.Fatalf("hash admin password: %v", err)
	}
	if _, err := store.EnsureUser(ctx, adminUser, hash, "Administrator", auth.RoleAdmin, time.Now().UTC()); err != nil {
		logger.Fatalf("seed admin user: %v", err)
	}
	if os.Getenv("ADMIN_PASSWORD") == "" {
		logger.Printf("WARNING: using default admin credentials %s/%s; set ADMIN_PASSWORD", adminUser, adminPass)
	}

	svr := api.New(store, api.ServerConfig{
		SLATargetDefaultHours: slaHours,
		SessionHours:          sessionHours,
		Logger:                logger,
	})

	// Automatic escalation worker: raises escalations for processes that
	// breached their SLA or dwelled past a stage budget. SWI_AUTO_ESCALATE_MINUTES
	// configures the interval (0 disables the worker).
	escalateMinutes, err := strconv.Atoi(envOr("SWI_AUTO_ESCALATE_MINUTES", "15"))
	if err != nil || escalateMinutes < 0 {
		logger.Printf("invalid SWI_AUTO_ESCALATE_MINUTES value %q, using 15", os.Getenv("SWI_AUTO_ESCALATE_MINUTES"))
		escalateMinutes = 15
	}
	if escalateMinutes > 0 {
		go func() {
			interval := time.Duration(escalateMinutes) * time.Minute
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					raised, err := store.AutoEscalate(ctx, time.Now().UTC())
					if err != nil {
						logger.Printf("auto escalate: %v", err)
						continue
					}
					for _, esc := range raised {
						logger.Printf("auto escalated order %d (%s) to %s", esc.OrderID, esc.OrderNumber, esc.Level)
					}
					if len(raised) > 0 {
						logger.Printf("auto escalate: raised %d escalation(s)", len(raised))
					}
				}
			}
		}()
		logger.Printf("auto escalation worker enabled (every %d minute(s))", escalateMinutes)
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           svr.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s (db: %s)", httpServer.Addr, dbPath)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("shutdown: %v", err)
	}
	logger.Printf("bye")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
