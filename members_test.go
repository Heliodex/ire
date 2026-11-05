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

// TestReviewerAvatarsAreExposed covers the avatar URL on the session, roster, profile and audit payloads.
func TestReviewerAvatarsAreExposed(t *testing.T) {
	application := newTestApp(t)
	admin := login(t, application, "admin@example.com")

	const avatar = "https://cdn.example.com/ada.png"

	if _, err := application.store.Exec(t.Context(), `update user set avatar_url = ? where email = 'admin@example.com'`, avatar); err != nil {
		t.Fatal(err)
	}

	me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, admin, nil))
	if me.User.AvatarURL == nil || *me.User.AvatarURL != avatar {
		t.Fatalf("session avatar = %+v", me.User.AvatarURL)
	}

	roster := decode[contract.MembersResponse](t, do(t, application, http.MethodGet, testBase+"/members", nil, admin, nil))

	var found bool

	for _, member := range roster.Members {
		if member.Email != "admin@example.com" {
			continue
		}

		found = true

		if member.AvatarURL == nil || *member.AvatarURL != avatar {
			t.Fatalf("member avatar = %+v", member.AvatarURL)
		}
	}

	if !found {
		t.Fatal("admin missing from roster")
	}

	profile := decode[contract.ReviewerProfile](t, do(t, application, http.MethodGet, testBase+"/reviewers/"+me.User.ID, nil, admin, nil))
	if profile.AvatarURL == nil || *profile.AvatarURL != avatar {
		t.Fatalf("profile avatar = %+v", profile.AvatarURL)
	}

	invite, _ := json.Marshal(contract.InviteMember{Email: "avatar-test@example.com", Role: roleReviewer})

	if created := do(t, application, http.MethodPost, testBase+"/members", invite, admin, nil); created.Code != http.StatusCreated {
		t.Fatalf("invite = %d: %s", created.Code, created.Body.String())
	}

	audit := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit", nil, admin, nil))
	for _, entry := range audit.Entries {
		if entry.Action != "member.invite" || entry.ActorEmail == nil || *entry.ActorEmail != "admin@example.com" {
			continue
		}

		if entry.ActorAvatarURL == nil || *entry.ActorAvatarURL != avatar {
			t.Fatalf("audit avatar = %+v", entry.ActorAvatarURL)
		}

		return
	}

	t.Fatal("invite audit entry missing")
}
