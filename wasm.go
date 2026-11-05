//go:build wasip1

package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
	wasihttp "github.com/heliodex/better-deploy/wt/packages/v2/wasihttp"
)

// loadConfig reads the prototype's configuration. Both development fixtures and development sign-in are opt-in (set to "1" locally); production deploys leave them unset or "0" so only Hack Club Auth for roster members can sign in.
func loadConfig() config {
	seed := os.Getenv("IRE_SEED_FIXTURES") == "1"

	return config{
		IngestSecret:      envOr("IRE_INGEST_SECRET", "dev-ingest-secret"),
		OutboundSecret:    envOr("IRE_OUTBOUND_SECRET", "dev-outbound-secret"),
		OutboundURL:       envOr("IRE_OUTBOUND_URL", "fake://receiver"),
		GitHubToken:       envOr("IRE_GITHUB_TOKEN", ""),
		GitHubAPIBase:     envOr("IRE_GITHUB_API", githubDefaultAPI),
		HackatimeAPIBase:  envOr("HACKATIME_API_BASE", hackatimeDefaultAPI),
		HCClientID:        envOr("HC_CLIENT_ID", ""),
		HCClientSecret:    envOr("HC_CLIENT_SECRET", ""),
		HCRedirectURI:     envOr("HC_REDIRECT_URI", ""),
		HCAuthBase:        envOr("HC_AUTH_BASE", "https://auth.hackclub.com"),
		BootstrapEmail:    strings.ToLower(strings.TrimSpace(os.Getenv("IRE_BOOTSTRAP_EMAIL"))),
		BootstrapRole:     envOr("IRE_BOOTSTRAP_ROLE", roleAdmin),
		SeedFixtures:      seed,
		AllowDevAccounts:  os.Getenv("IRE_DEV_AUTH") == "1",
		DisableRepoChecks: os.Getenv("IRE_DISABLE_REPO_CHECKS") == "1",
		EnableJobs:        os.Getenv("IRE_DISABLE_JOBS") != "1",
		OutboundDryRun:    os.Getenv("IRE_OUTBOUND_DRY_RUN") == "1",
		Shadow:            os.Getenv("IRE_SHADOW") == "1",
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
		application := newIreApp(store, wasihttp.NewClient(), loadConfig())
		handler = application.handler()
		migrate = application.migrate
	}

	wasihttp.Handle(app.ServeStorage(storeErr, migrate, handler).ServeHTTP)
}
