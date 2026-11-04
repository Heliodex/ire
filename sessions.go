package main

import (
	"errors"
	"net/http"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleRevokeSessions signs a member out everywhere after an admin request.
func (a *reviewApp) handleRevokeSessions(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	if person.Role != roleAdmin {
		app.WriteError(w, http.StatusForbidden, "only admins can revoke sessions")
		return
	}

	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	target := r.PathValue("id")

	var found int

	err := a.store.QueryRow(r.Context(), `select 1 from membership where program_id = ? and user_id = ?`, program.ID, target).Scan(&found)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown member")
			return
		}

		writeStorage(w, err)

		return
	}

	now := a.nowText()

	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{SQL: `delete from session where user_id = ?`, Args: []any{target}},
		storage.TransactionStatement{
			SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'user.sessions_revoke', 'user', ?, '{}', '{}', ?)`,
			Args: []any{newID("aud"), program.ID, person.ID, person.Email, target, now},
		},
	); err != nil {
		writeStorage(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
