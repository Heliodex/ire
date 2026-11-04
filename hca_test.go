package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"review/contract"
)

// fakeHCA is a minimal OpenID provider: discovery, token, JWKS and userinfo.
type fakeHCA struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	mu          sync.Mutex
	nonce       string
	email       string
	subject     string
	displayName string
}

// newFakeHCA starts the provider and returns its live URL.
func newFakeHCA(t *testing.T) *fakeHCA {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	provider := &fakeHCA{
		key:         key,
		email:       "reviewer@example.com",
		subject:     "ident!reviewer",
		displayName: "Rae Reviewer",
	}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(provider.server.Close)

	return provider
}

// setNonce records the nonce from the authorisation URL so the token can echo it.
func (f *fakeHCA) setNonce(nonce string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.nonce = nonce
}

// currentNonce reads the nonce under the lock.
func (f *fakeHCA) currentNonce() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.nonce
}

// handle serves the four endpoints the OAuth client uses.
func (f *fakeHCA) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeTestJSON(w, map[string]string{
			"issuer":                 f.server.URL,
			"authorization_endpoint": f.server.URL + "/oauth/authorize",
			"token_endpoint":         f.server.URL + "/oauth/token",
			"userinfo_endpoint":      f.server.URL + "/oauth/userinfo",
			"jwks_uri":               f.server.URL + "/oauth/keys",
		})
	case "/oauth/token":
		if err := r.ParseForm(); err != nil || r.Form.Get("code") != "good" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}

		writeTestJSON(w, map[string]any{
			"access_token": "hca-token",
			"expires_in":   300,
			"id_token":     f.idToken(),
			"token_type":   "Bearer",
		})
	case "/oauth/keys":
		writeTestJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": "k1",
			"n":   base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes()),
		}}})
	case "/oauth/userinfo":
		if r.Header.Get("Authorization") != "Bearer hca-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		writeTestJSON(w, map[string]string{
			"sub":   f.subject,
			"email": f.email,
			"name":  f.displayName,
		})
	default:
		http.NotFound(w, r)
	}
}

// idToken signs a compact RS256 JWT with the provider key.
func (f *fakeHCA) idToken() string {
	segment := func(value any) string {
		data, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(data)
	}

	now := time.Now()
	header := segment(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	claims := segment(map[string]any{
		"iss":   f.server.URL,
		"aud":   "client",
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
		"sub":   f.subject,
		"email": f.email,
		"name":  f.displayName,
		"nonce": f.currentNonce(),
	})

	signing := header + "." + claims
	digest := sha256.Sum256([]byte(signing))

	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		return ""
	}

	return signing + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// writeTestJSON writes a small JSON body for the fake provider.
func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")

	data, _ := json.Marshal(value)
	_, _ = w.Write(data)
}

// TestHCAOAuthFlow covers discovery, PKCE, state, the signed ID token, userinfo and the session cookie.
func TestHCAOAuthFlow(t *testing.T) {
	provider := newFakeHCA(t)
	application := newTestApp(t)
	application.config.HCClientID = "client"
	application.config.HCClientSecret = "secret"
	application.config.HCRedirectURI = "http://review.localhost/auth/callback"
	application.config.HCAuthBase = provider.server.URL

	start := do(t, application, http.MethodGet, "/auth/login", nil, nil, nil)
	if start.Code != http.StatusFound {
		t.Fatalf("start = %d: %s", start.Code, start.Body.String())
	}

	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	if location.Query().Get("code_challenge") == "" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorize = %s", location)
	}

	if location.Query().Get("nonce") == "" || location.Query().Get("redirect_uri") != application.config.HCRedirectURI {
		t.Fatalf("authorize = %s", location)
	}

	state := location.Query().Get("state")
	if state == "" || location.Query().Get("client_id") != "client" {
		t.Fatalf("authorize = %s", location)
	}

	provider.setNonce(location.Query().Get("nonce"))

	stateCookie := findCookie(start.Result().Cookies(), oauthStateCookie)
	if stateCookie == nil {
		t.Fatal("missing state cookie")
	}

	callback := do(t, application, http.MethodGet, "/auth/callback?code=good&state="+url.QueryEscape(state), nil, stateCookie, nil)
	if callback.Code != http.StatusFound {
		t.Fatalf("callback = %d: %s", callback.Code, callback.Body.String())
	}

	if callback.Header().Get("Location") != "/queue" {
		t.Fatalf("callback redirect = %q", callback.Header().Get("Location"))
	}

	sessionCookie := findCookie(callback.Result().Cookies(), cookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("missing session cookie")
	}

	me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, sessionCookie, nil))
	if me.User.Email != "reviewer@example.com" {
		t.Fatalf("me = %+v", me.User)
	}

	replay := do(t, application, http.MethodGet, "/auth/callback?code=good&state="+url.QueryEscape(state), nil, stateCookie, nil)
	if replay.Header().Get("Location") != "/?error=state" {
		t.Fatalf("replay redirect = %q", replay.Header().Get("Location"))
	}
}

// TestHCARejectsBadState covers a callback that does not match the browser cookie.
func TestHCARejectsBadState(t *testing.T) {
	provider := newFakeHCA(t)
	application := newTestApp(t)
	application.config.HCClientID = "client"
	application.config.HCClientSecret = "secret"
	application.config.HCRedirectURI = "http://review.localhost/auth/callback"
	application.config.HCAuthBase = provider.server.URL

	start := do(t, application, http.MethodGet, "/auth/login", nil, nil, nil)
	stateCookie := findCookie(start.Result().Cookies(), oauthStateCookie)

	callback := do(t, application, http.MethodGet, "/auth/callback?code=good&state=wrong", nil, stateCookie, nil)
	if callback.Header().Get("Location") != "/?error=state" {
		t.Fatalf("callback redirect = %q", callback.Header().Get("Location"))
	}
}

// TestHCARejectsUnknownMember covers the roster gate.
func TestHCARejectsUnknownMember(t *testing.T) {
	provider := newFakeHCA(t)
	provider.email = "nobody@example.com"

	application := newTestApp(t)
	application.config.HCClientID = "client"
	application.config.HCClientSecret = "secret"
	application.config.HCRedirectURI = "http://review.localhost/auth/callback"
	application.config.HCAuthBase = provider.server.URL

	start := do(t, application, http.MethodGet, "/auth/login", nil, nil, nil)
	location, _ := url.Parse(start.Header().Get("Location"))
	provider.setNonce(location.Query().Get("nonce"))

	stateCookie := findCookie(start.Result().Cookies(), oauthStateCookie)

	callback := do(t, application, http.MethodGet, "/auth/callback?code=good&state="+url.QueryEscape(location.Query().Get("state")), nil, stateCookie, nil)
	if callback.Header().Get("Location") != "/?error=not_member" {
		t.Fatalf("callback redirect = %q", callback.Header().Get("Location"))
	}

	if cookie := findCookie(callback.Result().Cookies(), cookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("unknown member received a session")
	}
}

// TestHCABootstrapsConfiguredEmail covers the first roster entry without opening the roster to everyone.
func TestHCABootstrapsConfiguredEmail(t *testing.T) {
	provider := newFakeHCA(t)
	provider.email = "new@example.com"
	provider.displayName = "New Reviewer"

	application := newTestApp(t)
	application.config.HCClientID = "client"
	application.config.HCClientSecret = "secret"
	application.config.HCRedirectURI = "http://review.localhost/auth/callback"
	application.config.HCAuthBase = provider.server.URL
	application.config.BootstrapEmail = "new@example.com"
	application.config.BootstrapRole = roleOrganizer

	start := do(t, application, http.MethodGet, "/auth/login", nil, nil, nil)
	location, _ := url.Parse(start.Header().Get("Location"))
	provider.setNonce(location.Query().Get("nonce"))

	stateCookie := findCookie(start.Result().Cookies(), oauthStateCookie)

	callback := do(t, application, http.MethodGet, "/auth/callback?code=good&state="+url.QueryEscape(location.Query().Get("state")), nil, stateCookie, nil)
	if callback.Header().Get("Location") != "/queue" {
		t.Fatalf("callback redirect = %q", callback.Header().Get("Location"))
	}

	sessionCookie := findCookie(callback.Result().Cookies(), cookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("bootstrap did not create a session")
	}

	me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, sessionCookie, nil))
	if me.User.Email != "new@example.com" || len(me.Programs) != 1 || me.Programs[0].Role != roleOrganizer {
		t.Fatalf("me = %+v", me)
	}

	// A second sign-in reuses the account rather than creating another membership.
	start = do(t, application, http.MethodGet, "/auth/login", nil, nil, nil)
	location, _ = url.Parse(start.Header().Get("Location"))
	provider.setNonce(location.Query().Get("nonce"))

	stateCookie = findCookie(start.Result().Cookies(), oauthStateCookie)

	callback = do(t, application, http.MethodGet, "/auth/callback?code=good&state="+url.QueryEscape(location.Query().Get("state")), nil, stateCookie, nil)
	if callback.Header().Get("Location") != "/queue" {
		t.Fatalf("second callback redirect = %q", callback.Header().Get("Location"))
	}

	var memberships int
	if err := application.store.QueryRow(t.Context(), `select count(*) from membership where user_id = (select id from user where email = ?)`, "new@example.com").Scan(&memberships); err != nil {
		t.Fatal(err)
	}

	if memberships != 1 {
		t.Errorf("memberships = %d, want 1", memberships)
	}
}

// TestHCASignInRejectsForeignOrigin covers the cross-site guard on development sign-in.
func TestHCASignInRejectsForeignOrigin(t *testing.T) {
	application := newTestApp(t)

	recorder := do(t, application, http.MethodPost, "/api/v1/dev/login", []byte(`{"email":"reviewer@example.com"}`), nil,
		map[string]string{"Origin": "https://evil.example"})
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-site login = %d: %s", recorder.Code, recorder.Body.String())
	}
}
