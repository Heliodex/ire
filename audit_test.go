package main

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"review/contract"
)

// TestAuditLogPage covers auditor access, action/search/age filters, target labels and before/after detail.
func TestAuditLogPage(t *testing.T) {
	application := newTestApp(t)

	if _, err := application.store.Exec(t.Context(), `insert into user (id, email, name, created_at) values ('usr_auditor', 'auditor@example.com', 'Aud', ?)`, application.nowText()); err != nil {
		t.Fatal(err)
	}
	if _, err := application.store.Exec(t.Context(), `insert into membership (id, program_id, user_id, role, tracks, permissions, active, created_at)
		values ('mem_auditor', ?, 'usr_auditor', 'auditor', '["hardware"]', '[]', 1, ?)`, programID, application.nowText()); err != nil {
		t.Fatal(err)
	}

	admin := login(t, application, "admin@example.com")
	reviewer := login(t, application, "reviewer@example.com")
	auditor := login(t, application, "auditor@example.com")

	if denied := do(t, application, http.MethodGet, testBase+"/audit", nil, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer audit = %d: %s", denied.Code, denied.Body.String())
	}

	invite, _ := json.Marshal(contract.InviteMember{Email: "audit@example.com", Role: roleReviewer})

	created := do(t, application, http.MethodPost, testBase+"/members", invite, admin, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("invite = %d: %s", created.Code, created.Body.String())
	}
	member := decode[contract.Member](t, created)

	update, _ := json.Marshal(contract.UpdateMember{Role: roleOrganizer, Active: true})

	updated := do(t, application, http.MethodPatch, testBase+"/members/"+member.ID, update, admin, nil)
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", updated.Code, updated.Body.String())
	}

	claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	decision, _ := json.Marshal(contract.DecisionRequest{
		Decision:       decisionApproved,
		IdempotencyKey: "audit-page-decision",
		NoteToMaker:    "Clean board.",
		Version:        1,
	})

	if decided := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", decision, reviewer, nil); decided.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", decided.Code, decided.Body.String())
	}

	all := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit", nil, auditor, nil))
	if len(all.Entries) < 3 {
		t.Fatalf("auditor audit = %+v", all.Entries)
	}

	members := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit?action=member.update", nil, admin, nil))
	if len(members.Entries) != 1 {
		t.Fatalf("member.update audit = %+v", members.Entries)
	}

	memberEntry := members.Entries[0]
	if memberEntry.TargetLabel == nil || *memberEntry.TargetLabel != "audit@example.com" {
		t.Errorf("member target label = %v", memberEntry.TargetLabel)
	}
	if memberEntry.BeforeJson == nil || *memberEntry.BeforeJson == "" || memberEntry.AfterJson == nil {
		t.Errorf("member before/after = %+v", memberEntry)
	}

	reviews := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit?action=review.decision", nil, auditor, nil))
	if len(reviews.Entries) != 1 {
		t.Fatalf("review.decision audit = %+v", reviews.Entries)
	}
	if reviews.Entries[0].TargetLabel == nil || *reviews.Entries[0].TargetLabel != "Starbie" {
		t.Errorf("review target label = %v", reviews.Entries[0].TargetLabel)
	}

	search := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit?q="+url.QueryEscape("audit@example.com"), nil, admin, nil))
	if len(search.Entries) == 0 || !strings.HasPrefix(search.Entries[0].Action, "member.") {
		t.Errorf("search audit = %+v", search.Entries)
	}

	if bad := do(t, application, http.MethodGet, testBase+"/audit?days=0", nil, admin, nil); bad.Code != http.StatusBadRequest {
		t.Errorf("days=0 = %d: %s", bad.Code, bad.Body.String())
	}

	paged := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit?limit=1", nil, admin, nil))
	if len(paged.Entries) != 1 || !paged.HasMore {
		t.Errorf("paged audit = %+v", paged)
	}

	recent := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit?days=1", nil, admin, nil))
	if len(recent.Entries) == 0 {
		t.Error("days=1 returned no entries")
	}
}
