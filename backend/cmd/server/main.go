package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"social-notes/backend/internal/config"
	"social-notes/backend/internal/httpapi"
	"social-notes/backend/internal/store"
)

func main() {
	envPath := flag.String("env", ".env", "path to environment file")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		log.Fatal(err)
	}
	databasePath := cfg.DatabasePath
	if !filepath.IsAbs(databasePath) {
		databasePath = filepath.Join(filepath.Dir(*envPath), databasePath)
	}
	db, err := store.Open(databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.Handler(db, httpapi.Options{
			SecureCookies: cfg.SessionCookieSecure, InstagramAccountID: cfg.InstagramDedicatedAccountID, InstagramUsername: cfg.InstagramDedicatedUsername, InstagramAccessToken: cfg.InstagramAccessToken,
			InstagramUser1AccountID: cfg.InstagramUser1AccountID, InstagramAccessUser1Token: cfg.InstagramAccessUser1Token,
			InstagramUser2AccountID: cfg.InstagramUser2AccountID, InstagramAccessUser2Token: cfg.InstagramAccessUser2Token,
			InboxOwnerEmail:             cfg.InstagramInboxOwnerEmail,
			InstagramWebhookVerifyToken: cfg.InstagramWebhookVerifyToken, InstagramAppSecret: cfg.InstagramAppSecret,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("API listening on http://%s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	<-stop.Done()
	ctx, shutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdown()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
