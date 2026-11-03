package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/app"
	"github.com/heliodex/better-deploy/wt/packages/storage"
)

// reviewer is the signed-in person and the membership the request acts through.
type reviewer struct {
	ID        string
	Email     string
	Name      string
	Role      string
	ProgramID string
}

// canReviewSecondPass reports whether the role may act on a held second-pass ship. Fraud and final approval are organiser work, never a stage 1 reviewer's.
func canReviewSecondPass(role string) bool {
	return role == roleOrganizer || role == roleAdmin
}

// currentReviewer reads the session cookie and resolves it to an active membership. It writes the 401 itself so handlers only branch on the boolean.
func (a *reviewApp) currentReviewer(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		app.WriteError(w, http.StatusUnauthorized, "sign in to review")
		return reviewer{}, false
	}

	sessionID := sessionHash(cookie.Value)

	row := a.store.QueryRow(r.Context(), `select u.id, u.email, coalesce(u.name, ''), m.role, m.program_id
		from session s
		join user u on u.id = s.user_id
		join membership m on m.user_id = u.id
		join program p on p.id = m.program_id
		where s.id = ? and s.expires_at > ? and m.active = 1 and p.active = 1
		order by s.last_seen_at desc
		limit 1`, sessionID, a.nowText())

	var person reviewer
	if err := row.Scan(&person.ID, &person.Email, &person.Name, &person.Role, &person.ProgramID); err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusUnauthorized, "session expired, sign in again")
			return reviewer{}, false
		}

		writeStorage(w, err)

		return reviewer{}, false
	}

	return person, true
}

// requireReviewer is currentReviewer plus a check that the membership can review this programme.
func (a *reviewApp) requireReviewer(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	person, ok := a.currentReviewer(w, r)
	if !ok {
		return reviewer{}, false
	}

	if program, ok := a.programFor(w, r); ok && program.ID != person.ProgramID {
		app.WriteError(w, http.StatusForbidden, "you do not belong to this programme")
		return reviewer{}, false
	}

	return person, true
}

// handleMe returns the signed-in reviewer and the programmes they can read.
func (a *reviewApp) handleMe(w http.ResponseWriter, r *http.Request) {
	person, ok := a.currentReviewer(w, r)
	if !ok {
		return
	}

	app.WriteJSON(w, http.StatusOK, a.meResponse(r.Context(), person))
}

// meResponse builds the shared sign-in response.
func (a *reviewApp) meResponse(ctx context.Context, person reviewer) contract.MeResponse {
	response := contract.MeResponse{
		User: contract.MeResponseUser{
			ID:    person.ID,
			Email: person.Email,
			Name:  stringPtr(person.Name),
		},
		Programs: []contract.Membership{},
	}

	rows, err := a.store.Query(ctx, `select p.id, p.slug, p.name, m.role
		from membership m join program p on p.id = m.program_id
		where m.user_id = ? and m.active = 1 and p.active = 1
		order by p.name`, person.ID)
	if err != nil {
		return response
	}
	defer rows.Close()

	for rows.Next() {
		var item contract.Membership
		if err := rows.Scan(&item.ProgramID, &item.ProgramSlug, &item.ProgramName, &item.Role); err != nil {
			continue
		}

		response.Programs = append(response.Programs, item)
	}

	return response
}

// handleDevLogin is the placeholder for Hack Club Auth. It issues a session for a seeded account so the queue can be demonstrated without an OAuth app; REVIEW_DEV_AUTH=0 removes it.
func (a *reviewApp) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	if !a.config.AllowDevAccounts {
		app.WriteError(w, http.StatusNotFound, "development sign-in is disabled")
		return
	}

	var request contract.LoginRequest
	if !app.ReadJSON(w, r, maxAPIBody, "email", &request) {
		return
	}

	email := strings.ToLower(strings.TrimSpace(request.Email))

	var person reviewer

	err := a.store.QueryRow(r.Context(), `select u.id, u.email, coalesce(u.name, ''), m.role, m.program_id
		from user u join membership m on m.user_id = u.id
		where u.email = ? and m.active = 1
		limit 1`, email).
		Scan(&person.ID, &person.Email, &person.Name, &person.Role, &person.ProgramID)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusUnauthorized, "no reviewer account with that email")
			return
		}

		writeStorage(w, err)

		return
	}

	token := newToken()

	now := a.nowText()
	if _, err := a.store.Exec(r.Context(), `insert into session (id, user_id, expires_at, created_at, last_seen_at, user_agent)
		values (?, ?, ?, ?, ?, ?)`,
		sessionHash(token), person.ID, a.later(sessionTTL), now, now, r.UserAgent()); err != nil {
		writeStorage(w, err)
		return
	}

	if _, err := a.store.Exec(r.Context(), `update user set last_login_at = ? where id = ?`, now, person.ID); err != nil {
		writeStorage(w, err)
		return
	}

	//nolint:gosec // Secure follows the request scheme behind the supervisor proxy.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   int(sessionTTL.Seconds()),
	})

	app.WriteJSON(w, http.StatusOK, a.meResponse(r.Context(), person))
}

// handleDevLogout deletes the session row and clears the cookie.
func (a *reviewApp) handleDevLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
		if _, err := a.store.Exec(r.Context(), `delete from session where id = ?`, sessionHash(cookie.Value)); err != nil {
			writeStorage(w, err)
			return
		}
	}

	//nolint:gosec // Secure follows the request scheme behind the supervisor proxy.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusNoContent)
}

// newToken returns 32 random bytes as hex. The database stores only its hash.
func newToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return newID("tok")
	}

	return hex.EncodeToString(raw[:])
}

// sessionHash returns the hex SHA-256 of the raw cookie token.
func sessionHash(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// requestIsHTTPS reports whether the request reached the app over TLS, including behind the supervisor proxy.
func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
