package main

import (
	"context"
	"net/http"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// shadowErrorCode is the conflict code a client sees when it tries to write in shadow mode.
const shadowErrorCode = "shadow_mode"

// requireWritable refuses a reviewer mutation while Review is a read-only shadow of Ari.
func (a *ireApp) requireWritable(w http.ResponseWriter) bool {
	if !a.config.Shadow {
		return true
	}

	app.WriteErrorCode(w, http.StatusConflict, shadowErrorCode, "Review is read-only while Ari owns decisions")

	return false
}

// requireReviewerWrite combines the session, shadow and role guards for a reviewer mutation.
func (a *ireApp) requireReviewerWrite(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return reviewer{}, false
	}

	if !a.requireWritable(w) {
		return reviewer{}, false
	}

	if !canReview(person.Role) {
		app.WriteError(w, http.StatusForbidden, "your role can only read reviews")
		return reviewer{}, false
	}

	return person, true
}

// outboundEnabled reports whether the programme may post a delivery right now.
func (a *ireApp) outboundEnabled(ctx context.Context, programID string) bool {
	if a.config.Shadow {
		return false
	}

	var enabled int

	if err := a.store.QueryRow(ctx, `select outbound_enabled from program where id = ?`, programID).Scan(&enabled); err != nil {
		return false
	}

	return enabled == 1
}
