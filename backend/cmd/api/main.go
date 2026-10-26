package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dncs/backend/internal/appointment"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/billing"
	"dncs/backend/internal/clinic"
	"dncs/backend/internal/config"
	"dncs/backend/internal/database"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/httpapi"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/patient"
	"dncs/backend/internal/prescription"
	"dncs/backend/internal/scheduling"
	"dncs/backend/internal/servicecatalog"
	"dncs/backend/internal/settings"
	"dncs/backend/internal/staff"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelStartup()
	pool, err := database.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if cfg.MigrateOnStart {
		if err := database.Migrate(startupContext, pool, cfg.MigrationsDir); err != nil {
			logger.Error("migrate database", "error", err)
			os.Exit(1)
		}
	}

	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTokenTTL)
	authStore := auth.NewStore(pool)
	authService, err := auth.NewService(authStore, tokenManager, auth.NewPasswordManager(), auth.ServiceConfig{
		StaffRefreshTTL: cfg.StaffRefreshTTL, PatientRefreshTTL: cfg.PatientRefreshTTL,
		MaxLoginFailures: cfg.LoginMaxFailures, LoginLockDuration: cfg.LoginLockDuration,
	})
	if err != nil {
		logger.Error("initialize authentication", "error", err)
		os.Exit(1)
	}
	authorizationStore := authorization.NewStore(pool)
	passwordManager := auth.NewPasswordManager()
	settingsService := settings.NewService(pool)
	inventoryService := inventory.NewService(pool, authorizationStore)
	handler := httpapi.NewApplicationRouter(
		pool,
		auth.NewHandler(authService, logger),
		auth.NewAuthenticator(tokenManager, authStore, logger),
		authorizationStore,
		httpapi.Handlers{
			Staff:        staff.NewHandler(staff.NewService(pool, passwordManager), logger),
			Patient:      patient.NewHandler(patient.NewService(pool, passwordManager), authService, logger),
			Clinic:       clinic.NewHandler(clinic.NewService(pool), logger),
			Scheduling:   scheduling.NewHandler(scheduling.NewService(pool, authorizationStore), logger),
			Appointment:  appointment.NewHandler(appointment.NewService(pool, authorizationStore), logger),
			Service:      servicecatalog.NewHandler(servicecatalog.NewService(pool, authorizationStore), logger),
			Encounter:    encounter.NewHandler(encounter.NewService(pool, authorizationStore), logger),
			Inventory:    inventory.NewHandler(inventoryService, logger),
			Prescription: prescription.NewHandler(prescription.NewService(pool, authorizationStore), logger),
			Settings:     settings.NewHandler(settingsService, logger),
			Billing:      billing.NewHandler(billing.NewService(pool, authorizationStore, inventoryService, settingsService), logger),
		},
		logger,
		httpapi.Options{
			RequestTimeout: cfg.RequestTimeout, AuthRateLimitMax: cfg.AuthRateLimitMax,
			AuthRateLimitWindow: cfg.AuthRateWindow, AuditIPHashKey: cfg.AuditIPHashKey,
			CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		},
	)

	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout,
	}
	go func() {
		logger.Info("DNCS API started", "address", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve HTTP", "error", err)
			os.Exit(1)
		}
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-shutdownSignal.Done()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown HTTP server", "error", err)
	}
}
