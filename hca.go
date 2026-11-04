package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/oauth"
	"github.com/heliodex/better-deploy/wt/packages/v2/session"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const (
	oauthStateCookie = "review_oauth_state"
	oauthStateTTL    = 10 * time.Minute
	oauthTimeout     = 15 * time.Second
)

// loginFlow is the server-side half of one authorisation attempt: PKCE, nonce and the exact redirect used.
type loginFlow struct {
	Verifier    string
	Nonce       string
	RedirectURI string
}

// hcaConfigured reports whether the OAuth application has the credentials the flow needs.
func (a *reviewApp) hcaConfigured() bool {
	return strings.TrimSpace(a.config.HCClientID) != "" && strings.TrimSpace(a.config.HCClientSecret) != ""
}

// hcaIssuer returns the OIDC issuer, defaulting to Hack Club Auth.
func (a *reviewApp) hcaIssuer() string {
	base := strings.TrimSpace(a.config.HCAuthBase)
	if base == "" {
		base = "https://auth.hackclub.com"
	}

	return strings.TrimRight(base, "/")
}

// oauthClient builds a client for one flow. The package caches discovery and keys for the request.
func (a *reviewApp) oauthClient() (*oauth.Client, error) {
	return oauth.New(oauth.Config{
		Issuer:       a.hcaIssuer(),
		ClientID:     strings.TrimSpace(a.config.HCClientID),
		ClientSecret: strings.TrimSpace(a.config.HCClientSecret),
		HTTPClient:   a.client,
		Scopes:       strings.Fields("openid profile email"),
		Timeout:      oauthTimeout,
	})
}

// redirectURI prefers the registered callback and falls back to the browser-facing origin.
func (a *reviewApp) redirectURI(r *http.Request) string {
	if value := strings.TrimSpace(a.config.HCRedirectURI); value != "" {
		return value
	}

	return app.PublicURL(r) + "/auth/callback"
}

// handleAuthConfig tells the login screen which sign-in methods exist.
func (a *reviewApp) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	app.WriteJSON(w, http.StatusOK, contract.AuthConfig{
		Dev: a.config.AllowDevAccounts,
		Hca: a.hcaConfigured(),
	})
}

// handleHCAStart begins the OpenID Connect code flow with PKCE, nonce and a stored flow.
func (a *reviewApp) handleHCAStart(w http.ResponseWriter, r *http.Request) {
	if !a.hcaConfigured() {
		app.WriteError(w, http.StatusServiceUnavailable, "Hack Club Auth is not configured")
		return
	}

	client, err := a.oauthClient()
	if err != nil {
		app.WriteError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}

	state, err := oauth.NewNonce()
	if err != nil {
		app.WriteError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}

	verifier, challenge, err := oauth.NewPKCE()
	if err != nil {
		app.WriteError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}

	nonce, err := oauth.NewNonce()
	if err != nil {
		app.WriteError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}

	redirect := a.redirectURI(r)

	authoriseURL, err := client.AuthoriseURL(r.Context(), redirect, state, nonce, challenge)
	if err != nil {
		app.WriteError(w, http.StatusBadGateway, "the identity provider is unavailable")
		return
	}

	if err := a.saveLoginFlow(r.Context(), state, loginFlow{Verifier: verifier, Nonce: nonce, RedirectURI: redirect}); err != nil {
		writeStorage(w, err)
		return
	}

	session.Set(w, r, oauthStateCookie, state, oauthStateTTL)
	//nolint:gosec // The URL comes from the configured issuer discovery document, not from user input.
	http.Redirect(w, r, authoriseURL, http.StatusFound)
}

// handleHCACallback verifies state, exchanges the code, verifies the ID token and opens a session for a roster member.
func (a *reviewApp) handleHCACallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if !a.hcaConfigured() {
		app.WriteError(w, http.StatusServiceUnavailable, "Hack Club Auth is not configured")
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))

	cookie, cookieErr := r.Cookie(oauthStateCookie)
	if cookieErr != nil || state == "" || !session.Equal(cookie.Value, state) {
		a.redirectAuthError(w, r, "state")
		return
	}

	session.Clear(w, r, oauthStateCookie)

	flow, found, err := a.takeLoginFlow(r.Context(), state)
	if err != nil {
		writeStorage(w, err)
		return
	}

	if !found {
		a.redirectAuthError(w, r, "state")
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		a.redirectAuthError(w, r, "code")
		return
	}

	client, err := a.oauthClient()
	if err != nil {
		a.redirectAuthError(w, r, "provider")
		return
	}

	tokens, err := client.Exchange(r.Context(), code, flow.RedirectURI, flow.Verifier)
	if err != nil {
		a.redirectAuthError(w, r, "exchange")
		return
	}

	identity, err := client.VerifyIDToken(r.Context(), tokens.IDToken, flow.Nonce)
	if err != nil {
		a.redirectAuthError(w, r, "token")
		return
	}

	if userinfo, infoErr := client.Userinfo(r.Context(), tokens.AccessToken); infoErr == nil {
		if mergeErr := identity.Merge(userinfo); mergeErr != nil {
			a.logAuthProblem("userinfo", mergeErr)
		}
	} else {
		a.logAuthProblem("userinfo", infoErr)
	}

	email := strings.ToLower(strings.TrimSpace(identity.Email))
	if email == "" {
		a.redirectAuthError(w, r, "identity")
		return
	}

	person, found, err := a.memberByEmail(r.Context(), email)
	if err != nil {
		writeStorage(w, err)
		return
	}

	if !found {
		a.redirectAuthError(w, r, "not_member")
		return
	}

	if person.Name == "" && identity.Name != "" {
		_, _ = a.store.Exec(r.Context(), `update user set name = ? where id = ?`, identity.Name, person.ID)
	}

	_, _ = a.store.Exec(r.Context(), `update user set last_login_at = ? where id = ?`, a.nowText(), person.ID)

	token, err := a.createSession(r.Context(), person.ID, r.UserAgent())
	if err != nil {
		writeStorage(w, err)
		return
	}

	reviewSessions.Set(w, r, token)
	http.Redirect(w, r, "/queue", http.StatusFound)
}

// saveLoginFlow stores one attempt and drops expired rows.
func (a *reviewApp) saveLoginFlow(ctx context.Context, state string, flow loginFlow) error {
	_, err := a.store.Transaction(ctx,
		storage.TransactionStatement{
			SQL:  `delete from login_flow where created_at < ?`,
			Args: []any{a.later(-oauthStateTTL)},
		},
		storage.TransactionStatement{
			SQL:  `insert into login_flow (state, verifier, nonce, redirect_uri, created_at) values (?, ?, ?, ?, ?)`,
			Args: []any{state, flow.Verifier, flow.Nonce, flow.RedirectURI, a.nowText()},
		},
	)

	return err
}

// takeLoginFlow consumes an attempt so a callback code cannot be replayed.
func (a *reviewApp) takeLoginFlow(ctx context.Context, state string) (loginFlow, bool, error) {
	var (
		flow      loginFlow
		createdAt string
	)

	err := a.store.QueryRow(ctx, `select verifier, nonce, redirect_uri, created_at from login_flow where state = ?`, state).
		Scan(&flow.Verifier, &flow.Nonce, &flow.RedirectURI, &createdAt)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return loginFlow{}, false, nil
		}

		return loginFlow{}, false, err
	}

	if _, err := a.store.Exec(ctx, `delete from login_flow where state = ?`, state); err != nil {
		return loginFlow{}, false, err
	}

	created, err := time.Parse(timestampFormat, createdAt)
	if err != nil || a.now().Sub(created) > oauthStateTTL {
		return loginFlow{}, false, nil
	}

	return flow, true, nil
}

// memberByEmail finds an active programme membership for an email.
func (a *reviewApp) memberByEmail(ctx context.Context, email string) (reviewer, bool, error) {
	var person reviewer

	err := a.store.QueryRow(ctx, `select u.id, u.email, coalesce(u.name, ''), m.role, m.program_id
		from user u
		join membership m on m.user_id = u.id
		join program p on p.id = m.program_id
		where lower(u.email) = ? and m.active = 1 and p.active = 1
		order by p.name limit 1`, email).
		Scan(&person.ID, &person.Email, &person.Name, &person.Role, &person.ProgramID)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return reviewer{}, false, nil
		}

		return reviewer{}, false, err
	}

	return person, true, nil
}

// redirectAuthError sends the browser back to the login screen with a short reason.
func (a *reviewApp) redirectAuthError(w http.ResponseWriter, r *http.Request, reason string) {
	reviewSessions.Clear(w, r)
	session.Clear(w, r, oauthStateCookie)
	http.Redirect(w, r, "/?error="+url.QueryEscape(reason), http.StatusFound)
}

// logAuthProblem keeps identity-merge failures out of the sign-in decision.
func (a *reviewApp) logAuthProblem(stage string, err error) {
	app.Warnf("hca %s: %v", stage, err)
}
