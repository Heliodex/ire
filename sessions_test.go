package main

import (
	"net/http"
	"testing"

	"ire/contract"
)

// TestRevokeSessions covers admin revocation and the role boundary.
func TestRevokeSessions(t *testing.T) {
	application := newTestApp(t)
	admin := login(t, application, "admin@example.com")
	reviewer := login(t, application, "reviewer@example.com")

	me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, reviewer, nil))

	if denied := do(t, application, http.MethodPost, testBase+"/members/"+me.User.ID+"/revoke-sessions", nil, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer revoke = %d: %s", denied.Code, denied.Body.String())
	}

	revoked := do(t, application, http.MethodPost, testBase+"/members/"+me.User.ID+"/revoke-sessions", nil, admin, nil)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("admin revoke = %d: %s", revoked.Code, revoked.Body.String())
	}

	if expired := do(t, application, http.MethodGet, "/api/v1/me", nil, reviewer, nil); expired.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session = %d: %s", expired.Code, expired.Body.String())
	}

	if missing := do(t, application, http.MethodPost, testBase+"/members/usr_missing/revoke-sessions", nil, admin, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing member = %d: %s", missing.Code, missing.Body.String())
	}
}
