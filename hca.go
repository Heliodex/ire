package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/app"
	"github.com/heliodex/better-deploy/wt/packages/storage"
)

const (
	oauthStateCookie = "review_oauth_state"
	oauthStateTTL    = 10 * time.Minute
	oauthScopes      = "openid profile email slack_id"
	oauthTimeout     = 15 * time.Second
)

// hcaTokens is the token response from Hack Club Auth.
type hcaTokens struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
}

// hcaIdentity is the identity response from Hack Club Auth.
type hcaIdentity struct {
	Identity struct {
		ID           string `json:"id"`
		PrimaryEmail string `json:"primary_email"`
		SlackID      string `json:"slack_id"`
	} `json:"identity"`
}

// hcaConfigured reports whether the OAuth application is fully configured.
func (a *reviewApp) hcaConfigured() bool {
	return a.config.HCClientID != "" && a.config.HCClientSecret != "" && a.config.HCRedirectURI != ""
}

// hcaBase returns the configured auth base without a trailing slash.
func (a *reviewApp) hcaBase() string {
	base := strings.TrimSpace(a.config.HCAuthBase)
	if base == "" {
		base = "https://auth.hackclub.com"
	}

	return strings.TrimRight(base, "/")
}

// handleAuthConfig tells the login screen which sign-in methods exist.
func (a *reviewApp) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	app.WriteJSON(w, http.StatusOK, contract.AuthConfig{
		Dev: a.config.AllowDevAccounts,
		Hca: a.hcaConfigured(),
	})
}

// handleHCAStart begins the Hack Club Auth code flow.
func (a *reviewApp) handleHCAStart(w http.ResponseWriter, r *http.Request) {
	if !a.hcaConfigured() {
		app.WriteError(w, http.StatusServiceUnavailable, "Hack Club Auth is not configured")
		return
	}

	state := newToken()

	//nolint:gosec // Secure follows the request scheme behind the supervisor proxy.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   int(oauthStateTTL.Seconds()),
	})

	authorize, err := url.Parse(a.hcaBase() + "/oauth/authorize")
	if err != nil {
		app.WriteError(w, http.StatusInternalServerError, "invalid auth base")
		return
	}

	query := authorize.Query()
	query.Set("client_id", a.config.HCClientID)
	query.Set("redirect_uri", a.config.HCRedirectURI)
	query.Set("response_type", "code")
	query.Set("scope", oauthScopes)
	query.Set("state", state)
	authorize.RawQuery = query.Encode()

	http.Redirect(w, r, authorize.String(), http.StatusFound)
}

// handleHCACallback verifies the state, exchanges the code and creates a session for a programme member.
func (a *reviewApp) handleHCACallback(w http.ResponseWriter, r *http.Request) {
	if !a.hcaConfigured() {
		app.WriteError(w, http.StatusServiceUnavailable, "Hack Club Auth is not configured")
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))

	cookie, cookieErr := r.Cookie(oauthStateCookie)
	if cookieErr != nil || state == "" || cookie.Value != state {
		a.redirectAuthError(w, r, "state")
		return
	}

	a.clearOAuthState(w, r)

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		a.redirectAuthError(w, r, "code")
		return
	}

	tokens, err := a.exchangeHCACode(r.Context(), code)
	if err != nil {
		a.redirectAuthError(w, r, "exchange")
		return
	}

	identity, err := a.fetchHCAIdentity(r.Context(), tokens.AccessToken)
	if err != nil {
		a.redirectAuthError(w, r, "identity")
		return
	}

	email := strings.ToLower(strings.TrimSpace(identity.Identity.PrimaryEmail))
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

	if identity.Identity.SlackID != "" {
		_, _ = a.store.Exec(r.Context(), `update user set slack_id = ? where id = ?`, identity.Identity.SlackID, person.ID)
	}

	if person.Name == "" {
		_, _ = a.store.Exec(r.Context(), `update user set name = ? where id = ?`, email, person.ID)
	}

	_, _ = a.store.Exec(r.Context(), `update user set last_login_at = ? where id = ?`, a.nowText(), person.ID)

	token, err := a.createSession(r.Context(), person.ID, r.UserAgent())
	if err != nil {
		writeStorage(w, err)
		return
	}

	a.setSessionCookie(w, r, token)
	http.Redirect(w, r, "/queue", http.StatusFound)
}

// exchangeHCACode exchanges an authorization code for an access token.
func (a *reviewApp) exchangeHCACode(ctx context.Context, code string) (hcaTokens, error) {
	var tokens hcaTokens

	payload, err := json.Marshal(map[string]string{
		"client_id":     a.config.HCClientID,
		"client_secret": a.config.HCClientSecret,
		"redirect_uri":  a.config.HCRedirectURI,
		"code":          code,
		"grant_type":    "authorization_code",
	})
	if err != nil {
		return tokens, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, a.hcaBase()+"/oauth/token", strings.NewReader(string(payload)))
	if err != nil {
		return tokens, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return tokens, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tokens, fmt.Errorf("hca token exchange: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return tokens, err
	}

	if err := json.Unmarshal(body, &tokens); err != nil {
		return tokens, err
	}

	if tokens.AccessToken == "" {
		return tokens, errors.New("hca token exchange: empty access token")
	}

	return tokens, nil
}

// fetchHCAIdentity loads the signed-in identity.
func (a *reviewApp) fetchHCAIdentity(ctx context.Context, accessToken string) (hcaIdentity, error) {
	var identity hcaIdentity

	requestCtx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, a.hcaBase()+"/api/v1/me", nil)
	if err != nil {
		return identity, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return identity, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return identity, fmt.Errorf("hca identity: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return identity, err
	}

	err = json.Unmarshal(body, &identity)

	return identity, err
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
	a.clearSessionCookie(w, r)
	a.clearOAuthState(w, r)
	http.Redirect(w, r, "/?error="+url.QueryEscape(reason), http.StatusFound)
}

// clearOAuthState removes the short-lived state cookie.
func (a *reviewApp) clearOAuthState(w http.ResponseWriter, r *http.Request) {
	//nolint:gosec // Secure follows the request scheme behind the supervisor proxy.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   -1,
	})
}
