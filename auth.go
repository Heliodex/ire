package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/session"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// reviewSessions owns the browser cookie name and lifetime shared by both sign-in methods.
var reviewSessions = session.New(cookieName, sessionTTL)

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

// canReview reports whether a role may claim, draft or decide. Auditors are read-only.
func canReview(role string) bool {
	return role == roleReviewer || canReviewSecondPass(role)
}

// currentReviewer reads the session cookie and resolves it to an active membership. It writes the 401 itself so handlers only branch on the boolean.
func (a *reviewApp) currentReviewer(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	token, ok := reviewSessions.Token(r)
	if !ok {
		app.WriteError(w, http.StatusUnauthorized, "sign in to review")
		return reviewer{}, false
	}

	sessionID := session.Hash(token)

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

// hasSession reports a live session without writing an error response.
func (a *reviewApp) hasSession(r *http.Request) bool {
	token, ok := reviewSessions.Token(r)
	if !ok {
		return false
	}

	var found int

	err := a.store.QueryRow(r.Context(), `select 1 from session s
		join user u on u.id = s.user_id
		join membership m on m.user_id = u.id
		join program p on p.id = m.program_id
		where s.id = ? and s.expires_at > ? and m.active = 1 and p.active = 1 limit 1`,
		session.Hash(token), a.nowText()).Scan(&found)

	return err == nil
}

// requireReviewer is currentReviewer plus a check that the membership can review this programme.
func (a *reviewApp) requireReviewer(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !session.SameOrigin(r) {
		app.WriteErrorCode(w, http.StatusForbidden, "forbidden", "cross-site request rejected")
		return reviewer{}, false
	}

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
		ReadOnly: a.config.Shadow,
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
	if !session.SameOrigin(r) {
		app.WriteErrorCode(w, http.StatusForbidden, "forbidden", "cross-site request rejected")
		return
	}

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

	token, err := a.createSession(r.Context(), person.ID, r.UserAgent())
	if err != nil {
		writeStorage(w, err)
		return
	}

	if _, err := a.store.Exec(r.Context(), `update user set last_login_at = ? where id = ?`, a.nowText(), person.ID); err != nil {
		writeStorage(w, err)
		return
	}

	reviewSessions.Set(w, r, token)
	app.WriteJSON(w, http.StatusOK, a.meResponse(r.Context(), person))
}

// handleLogout deletes the session row and clears the cookie.
func (a *reviewApp) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !session.SameOrigin(r) {
		app.WriteErrorCode(w, http.StatusForbidden, "forbidden", "cross-site request rejected")
		return
	}

	if token, ok := reviewSessions.Token(r); ok {
		if _, err := a.store.Exec(r.Context(), `delete from session where id = ?`, session.Hash(token)); err != nil {
			writeStorage(w, err)
			return
		}
	}

	reviewSessions.Clear(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// createSession stores a hashed session token and returns the raw cookie value.
func (a *reviewApp) createSession(ctx context.Context, userID, userAgent string) (string, error) {
	token, err := session.NewToken()
	if err != nil {
		return "", err
	}

	now := a.nowText()

	_, err = a.store.Exec(ctx, `insert into session (id, user_id, expires_at, created_at, last_seen_at, user_agent)
		values (?, ?, ?, ?, ?, ?)`,
		session.Hash(token), userID, a.later(sessionTTL), now, now, userAgent)

	return token, err
}
