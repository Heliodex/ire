//go:build wasip1

package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/heliodex/better-deploy/wt/packages/app"
	"github.com/heliodex/better-deploy/wt/packages/storage"
	wasihttp "github.com/heliodex/better-deploy/wt/packages/wasihttp"
)

// loadConfig reads the prototype's configuration. Missing secrets fall back to documented development values; production must set both.
func loadConfig() config {
	seed := os.Getenv("REVIEW_SEED_FIXTURES") != "0"

	return config{
		IngestSecret:      envOr("REVIEW_INGEST_SECRET", "dev-ingest-secret"),
		OutboundSecret:    envOr("REVIEW_OUTBOUND_SECRET", "dev-outbound-secret"),
		OutboundURL:       envOr("REVIEW_OUTBOUND_URL", "fake://receiver"),
		GitHubToken:       envOr("REVIEW_GITHUB_TOKEN", ""),
		GitHubAPIBase:     envOr("REVIEW_GITHUB_API", githubDefaultAPI),
		HCClientID:        envOr("HC_CLIENT_ID", ""),
		HCClientSecret:    envOr("HC_CLIENT_SECRET", ""),
		HCRedirectURI:     envOr("HC_REDIRECT_URI", ""),
		HCAuthBase:        envOr("HC_AUTH_BASE", "https://auth.hackclub.com"),
		SeedFixtures:      seed,
		AllowDevAccounts:  os.Getenv("REVIEW_DEV_AUTH") != "0",
		DisableRepoChecks: os.Getenv("REVIEW_DISABLE_REPO_CHECKS") == "1",
	}
}

// envOr returns the environment value or fallback when it is empty.
func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

// init is the only wasm-specific wiring: storage, the review routes, the outbound client and the wasi:http export.
func init() {
	store, storeErr := storage.New(wasihttp.NewClient())

	var (
		handler http.Handler
		migrate func(context.Context) error
	)

	if storeErr == nil {
		application := newReviewApp(store, wasihttp.NewClient(), loadConfig())
		handler = application.handler()
		migrate = application.migrate
	}

	wasihttp.Handle(app.ServeStorage(storeErr, migrate, handler).ServeHTTP)
}
