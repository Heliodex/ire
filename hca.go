package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ire/contract"

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
	Verifier, Nonce, RedirectURI string
}

// hcaConfigured reports whether the OAuth application has the credentials the flow needs.
func (a *ireApp) hcaConfigured() bool {
	return strings.TrimSpace(a.config.HCClientID) != "" && strings.TrimSpace(a.config.HCClientSecret) != ""
}

// hcaIssuer returns the OIDC issuer, defaulting to Hack Club Auth.
func (a *ireApp) hcaIssuer() string {
	base := strings.TrimSpace(a.config.HCAuthBase)
	if base == "" {
		base = "https://auth.hackclub.com"
	}

	return strings.TrimRight(base, "/")
}

// oauthClient builds a client for one flow. The package caches discovery and keys for the request.
func (a *ireApp) oauthClient() (*oauth.Client, error) {
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
func (a *ireApp) redirectURI(r *http.Request) string {
	if value := strings.TrimSpace(a.config.HCRedirectURI); value != "" {
		return value
	}

	return app.PublicURL(r) + "/auth/callback"
}

// safeAvatarURL accepts only absolute http(s) pictures from the identity provider.
func safeAvatarURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}

	return parsed.String()
}

// handleAuthConfig tells the login screen which sign-in methods exist.
func (a *ireApp) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	app.WriteJSON(w, http.StatusOK, contract.AuthConfig{
		Dev: a.config.AllowDevAccounts,
		Hca: a.hcaConfigured(),
	})
}

// handleHCAStart begins the OpenID Connect code flow with PKCE, nonce and a stored flow.
func (a *ireApp) handleHCAStart(w http.ResponseWriter, r *http.Request) {
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
func (a *ireApp) handleHCACallback(w http.ResponseWriter, r *http.Request) {
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

	person, found, err := a.memberBySubject(r.Context(), identity.Subject)
	if err != nil {
		writeStorage(w, err)
		return
	}

	if !found {
		person, found, err = a.memberByEmail(r.Context(), email)
		if err != nil {
			writeStorage(w, err)
			return
		}
	}

	if !found && a.canBootstrap(r.Context(), email) {
		person, found, err = a.bootstrapMember(r.Context(), email, identity.Name)
		if err != nil {
			writeStorage(w, err)
			return
		}
	}

	if !found {
		a.redirectAuthError(w, r, "not_member")
		return
	}

	if identity.Subject != "" {
		if err := a.bindIdentity(r.Context(), person.ID, "hca", identity.Subject); err != nil {
			if errors.Is(err, errIdentityConflict) {
				a.redirectAuthError(w, r, "identity_conflict")
				return
			}

			writeStorage(w, err)

			return
		}
	}

	if person.Name == "" && identity.Name != "" {
		_, _ = a.store.Exec(r.Context(), `update user set name = ? where id = ?`, identity.Name, person.ID)
	}

	if picture := safeAvatarURL(identity.Picture); picture != "" {
		_, _ = a.store.Exec(r.Context(), `update user set avatar_url = ? where id = ?`, picture, person.ID)
	}

	_, _ = a.store.Exec(r.Context(), `update user set last_login_at = ? where id = ?`, a.nowText(), person.ID)

	if old, ok := reviewSessions.Token(r); ok {
		_, _ = a.store.Exec(r.Context(), `delete from session where id = ?`, session.Hash(old))
	}

	token, err := a.createSession(r.Context(), person.ID, r.UserAgent())
	if err != nil {
		writeStorage(w, err)
		return
	}

	reviewSessions.Set(w, r, token)
	http.Redirect(w, r, "/stations", http.StatusFound)
}

// saveLoginFlow stores one attempt and drops expired rows.
func (a *ireApp) saveLoginFlow(ctx context.Context, state string, flow loginFlow) error {
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
func (a *ireApp) takeLoginFlow(ctx context.Context, state string) (loginFlow, bool, error) {
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
func (a *ireApp) memberByEmail(ctx context.Context, email string) (reviewer, bool, error) {
	var person reviewer

	var permissions string

	err := a.store.QueryRow(ctx, `select u.id, u.email, coalesce(u.name, ''), coalesce(u.avatar_url, ''), m.role, m.program_id, coalesce(rd.permissions, '[]')
		from user u
		join membership m on m.user_id = u.id
		join program p on p.id = m.program_id
		left join role_definition rd on rd.program_id = m.program_id and rd.name = m.role
		where lower(u.email) = ? and m.active = 1 and p.active = 1
		order by p.name limit 1`, email).
		Scan(&person.ID, &person.Email, &person.Name, &person.AvatarURL, &person.Role, &person.ProgramID, &permissions)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return reviewer{}, false, nil
		}

		return reviewer{}, false, err
	}

	person.Permissions = unmarshalStrings(permissions)

	return person, true, nil
}

// errIdentityConflict means an account is already bound to a different Hack Club identity.
var errIdentityConflict = errors.New("identity is already linked")

// memberBySubject finds an active membership through a stable provider subject.
func (a *ireApp) memberBySubject(ctx context.Context, subject string) (reviewer, bool, error) {
	if strings.TrimSpace(subject) == "" {
		return reviewer{}, false, nil
	}

	var person reviewer

	var permissions string

	err := a.store.QueryRow(ctx, `select u.id, u.email, coalesce(u.name, ''), coalesce(u.avatar_url, ''), m.role, m.program_id, coalesce(rd.permissions, '[]')
		from user_identity i
		join user u on u.id = i.user_id
		join membership m on m.user_id = u.id
		join program p on p.id = m.program_id
		left join role_definition rd on rd.program_id = m.program_id and rd.name = m.role
		where i.provider = 'hca' and i.subject = ? and m.active = 1 and p.active = 1
		order by p.name limit 1`, subject).
		Scan(&person.ID, &person.Email, &person.Name, &person.AvatarURL, &person.Role, &person.ProgramID, &permissions)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return reviewer{}, false, nil
		}

		return reviewer{}, false, err
	}

	person.Permissions = unmarshalStrings(permissions)

	return person, true, nil
}

// bindIdentity links a stable subject once and refuses a second subject for the same account.
func (a *ireApp) bindIdentity(ctx context.Context, userID, provider, subject string) error {
	if strings.TrimSpace(subject) == "" {
		return nil
	}

	var boundUser string

	err := a.store.QueryRow(ctx, `select user_id from user_identity where provider = ? and subject = ?`, provider, subject).Scan(&boundUser)
	if err == nil {
		if boundUser != userID {
			return errIdentityConflict
		}

		return nil
	}

	if !errors.Is(err, storage.ErrNoRows) {
		return err
	}

	var boundSubject string

	err = a.store.QueryRow(ctx, `select subject from user_identity where user_id = ? and provider = ?`, userID, provider).Scan(&boundSubject)
	if err == nil {
		if boundSubject != subject {
			return errIdentityConflict
		}

		return nil
	}

	if !errors.Is(err, storage.ErrNoRows) {
		return err
	}

	_, err = a.store.Exec(ctx, `insert or ignore into user_identity (user_id, provider, subject, created_at) values (?, ?, ?, ?)`,
		userID, provider, subject, a.nowText())

	return err
}

// canBootstrap reports whether this sign-in may create the first membership. Only one explicitly configured email may bootstrap; there is no wildcard, so unknown accounts can never create their own membership. The configured role must exist in the programme role definitions.
func (a *ireApp) canBootstrap(ctx context.Context, email string) bool {
	if a.config.BootstrapEmail == "" || a.config.BootstrapRole == "" {
		return false
	}

	if !strings.EqualFold(strings.TrimSpace(email), a.config.BootstrapEmail) {
		return false
	}

	return a.validMemberRole(ctx, programID, a.config.BootstrapRole)
}

// bootstrapMember creates the configured bootstrap account and its programme membership once.
func (a *ireApp) bootstrapMember(ctx context.Context, email, name string) (reviewer, bool, error) {
	var userID string

	err := a.store.QueryRow(ctx, `select id from user where lower(email) = ?`, email).Scan(&userID)
	if err != nil && !errors.Is(err, storage.ErrNoRows) {
		return reviewer{}, false, err
	}

	now := a.nowText()

	if userID == "" {
		userID = newID("usr")

		if _, err := a.store.Exec(ctx, `insert into user (id, email, name, created_at) values (?, ?, ?, ?)`,
			userID, email, name, now); err != nil {
			return reviewer{}, false, err
		}
	}

	var (
		membershipID string
		active       int
	)

	err = a.store.QueryRow(ctx, `select id, active from membership where program_id = ? and user_id = ?`, programID, userID).
		Scan(&membershipID, &active)
	if err != nil && !errors.Is(err, storage.ErrNoRows) {
		return reviewer{}, false, err
	}

	if err == nil {
		// An existing membership, including an inactive one, is never silently changed by bootstrap.
		return a.memberByEmail(ctx, email)
	}

	if _, err := a.store.Exec(ctx, `insert into membership (id, program_id, user_id, role, tracks, permissions, active, created_at, created_by)
		values (?, ?, ?, ?, '["hardware"]', '[]', 1, ?, 'bootstrap')`,
		newID("mem"), programID, userID, a.config.BootstrapRole, now); err != nil {
		return reviewer{}, false, err
	}

	_, _ = a.store.Exec(ctx, `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
		values (?, ?, ?, ?, 'user.bootstrap', 'user', ?, '{}', ?, ?)`,
		newID("aud"), programID, userID, email, userID, marshalMap(map[string]any{"role": a.config.BootstrapRole}), now)

	app.Infof("bootstrapped %s as %s", email, a.config.BootstrapRole)

	return a.memberByEmail(ctx, email)
}

// redirectAuthError sends the browser back to the login screen with a short reason.
func (a *ireApp) redirectAuthError(w http.ResponseWriter, r *http.Request, reason string) {
	reviewSessions.Clear(w, r)
	session.Clear(w, r, oauthStateCookie)
	http.Redirect(w, r, "/?error="+url.QueryEscape(reason), http.StatusFound)
}

// logAuthProblem keeps identity-merge failures out of the sign-in decision.
func (a *ireApp) logAuthProblem(stage string, err error) {
	app.Warnf("hca %s: %v", stage, err)
}
