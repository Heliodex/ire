package main

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"review/contract"
)

// TestMemberAdministration covers the roster, invite, update, audit and last-admin guard.
func TestMemberAdministration(t *testing.T) {
	application := newTestApp(t)
	admin := login(t, application, "admin@example.com")
	reviewer := login(t, application, "reviewer@example.com")

	if denied := do(t, application, http.MethodGet, testBase+"/members", nil, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer list = %d: %s", denied.Code, denied.Body.String())
	}

	roster := decode[contract.MembersResponse](t, do(t, application, http.MethodGet, testBase+"/members", nil, admin, nil))
	if len(roster.Members) != 3 {
		t.Fatalf("members = %+v", roster.Members)
	}

	invite, _ := json.Marshal(contract.InviteMember{Email: "New@Example.com", Role: roleOrganizer})

	created := do(t, application, http.MethodPost, testBase+"/members", invite, admin, map[string]string{
		"Origin": "http://example.com",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("invite = %d: %s", created.Code, created.Body.String())
	}

	member := decode[contract.Member](t, created)
	if member.Email != "new@example.com" || member.Role != roleOrganizer || !member.Active {
		t.Fatalf("invited = %+v", member)
	}

	update, _ := json.Marshal(contract.UpdateMember{Role: roleReviewer, Active: false})

	updated := do(t, application, http.MethodPatch, testBase+"/members/"+member.ID, update, admin, map[string]string{
		"Origin": "http://example.com",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", updated.Code, updated.Body.String())
	}

	changed := decode[contract.Member](t, updated)
	if changed.Role != roleReviewer || changed.Active {
		t.Fatalf("updated = %+v", changed)
	}

	audit := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit", nil, admin, nil))
	if len(audit.Entries) < 2 || !strings.HasPrefix(audit.Entries[0].Action, "member.") {
		t.Fatalf("audit = %+v", audit.Entries)
	}

	demote, _ := json.Marshal(contract.UpdateMember{Role: roleReviewer, Active: true})

	var adminID string

	if err := application.store.QueryRow(t.Context(), `select id from user where email = 'admin@example.com'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	if last := do(t, application, http.MethodPatch, testBase+"/members/"+adminID, demote, admin, nil); last.Code != http.StatusConflict {
		t.Fatalf("last admin = %d: %s", last.Code, last.Body.String())
	}
}
