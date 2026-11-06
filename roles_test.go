package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

// TestRoleManagement covers role listing, creation, permission edits, deletion
// guards and assignment of custom roles to members.
func TestRoleManagement(t *testing.T) {
	application := newTestApp(t)
	admin := login(t, application, "admin@example.com")
	reviewer := login(t, application, "reviewer@example.com")

	if denied := do(t, application, http.MethodGet, testBase+"/roles", nil, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer roles = %d: %s", denied.Code, denied.Body.String())
	}

	roles := decode[contract.RolesResponse](t, do(t, application, http.MethodGet, testBase+"/roles", nil, admin, nil))
	if len(roles.Roles) != 4 {
		t.Fatalf("roles = %+v", roles.Roles)
	}

	if !roles.Roles[0].Locked || len(roles.Roles[0].Permissions) != 1 || roles.Roles[0].Permissions[0] != permWildcard {
		t.Fatalf("admin role = %+v", roles.Roles[0])
	}

	origin := map[string]string{"Origin": "http://example.com"}

	create, _ := json.Marshal(contract.CreateRole{Name: "Stage2", Permissions: []string{permSecondPass, permReview}})

	created := do(t, application, http.MethodPost, testBase+"/roles", create, admin, origin)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}

	role := decode[contract.RoleDefinition](t, created)
	if role.Name != "stage2" || role.Locked || len(role.Permissions) != 2 {
		t.Fatalf("created = %+v", role)
	}

	if duplicate := do(t, application, http.MethodPost, testBase+"/roles", create, admin, origin); duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d: %s", duplicate.Code, duplicate.Body.String())
	}

	for name, body := range map[string][]byte{
		"bad name": func() []byte {
			raw, _ := json.Marshal(contract.CreateRole{Name: "No Spaces!", Permissions: []string{permReview}})
			return raw
		}(),
		"bad permission": func() []byte {
			raw, _ := json.Marshal(contract.CreateRole{Name: "sneaky", Permissions: []string{"do_anything"}})
			return raw
		}(),
		"wildcard": func() []byte {
			raw, _ := json.Marshal(contract.CreateRole{Name: "sneaky", Permissions: []string{permWildcard}})
			return raw
		}(),
	} {
		if rejected := do(t, application, http.MethodPost, testBase+"/roles", body, admin, origin); rejected.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d: %s", name, rejected.Code, rejected.Body.String())
		}
	}

	if denied := do(t, application, http.MethodPost, testBase+"/roles", create, reviewer, origin); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer create = %d: %s", denied.Code, denied.Body.String())
	}

	update, _ := json.Marshal(contract.UpdateRole{Permissions: []string{permSecondPass}})

	updated := do(t, application, http.MethodPatch, testBase+"/roles/stage2", update, admin, origin)
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", updated.Code, updated.Body.String())
	}

	if changed := decode[contract.RoleDefinition](t, updated); len(changed.Permissions) != 1 || changed.Permissions[0] != permSecondPass {
		t.Fatalf("updated = %+v", changed)
	}

	if locked := do(t, application, http.MethodPatch, testBase+"/roles/admin", update, admin, origin); locked.Code != http.StatusForbidden {
		t.Fatalf("locked update = %d: %s", locked.Code, locked.Body.String())
	}

	if missing := do(t, application, http.MethodPatch, testBase+"/roles/nobody", update, admin, origin); missing.Code != http.StatusNotFound {
		t.Fatalf("missing update = %d: %s", missing.Code, missing.Body.String())
	}

	invite, _ := json.Marshal(contract.InviteMember{Email: "custom@example.com", Role: "stage2"})

	member := decode[contract.Member](t, do(t, application, http.MethodPost, testBase+"/members", invite, admin, origin))
	if member.Role != "stage2" {
		t.Fatalf("invited = %+v", member)
	}

	badInvite, _ := json.Marshal(contract.InviteMember{Email: "ghost@example.com", Role: "nobody"})

	if unknown := do(t, application, http.MethodPost, testBase+"/members", badInvite, admin, origin); unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown role invite = %d: %s", unknown.Code, unknown.Body.String())
	}

	custom := login(t, application, "custom@example.com")
	me := decode[contract.MeResponse](t, do(t, application, http.MethodGet, "/api/v1/me", nil, custom, nil))

	if len(me.Programs) != 1 || len(me.Programs[0].Permissions) != 1 || me.Programs[0].Permissions[0] != permSecondPass {
		t.Fatalf("me = %+v", me.Programs)
	}

	if denied := do(t, application, http.MethodGet, testBase+"/members", nil, custom, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("custom roster = %d: %s", denied.Code, denied.Body.String())
	}

	if inUse := do(t, application, http.MethodDelete, testBase+"/roles/stage2", nil, admin, origin); inUse.Code != http.StatusConflict {
		t.Fatalf("delete in use = %d: %s", inUse.Code, inUse.Body.String())
	}

	move, _ := json.Marshal(contract.UpdateMember{Role: roleReviewer, Active: true})

	if moved := do(t, application, http.MethodPatch, testBase+"/members/"+member.ID, move, admin, origin); moved.Code != http.StatusOK {
		t.Fatalf("move = %d: %s", moved.Code, moved.Body.String())
	}

	if deleted := do(t, application, http.MethodDelete, testBase+"/roles/stage2", nil, admin, origin); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}

	if locked := do(t, application, http.MethodDelete, testBase+"/roles/admin", nil, admin, origin); locked.Code != http.StatusForbidden {
		t.Fatalf("locked delete = %d: %s", locked.Code, locked.Body.String())
	}

	audit := decode[contract.AuditResponse](t, do(t, application, http.MethodGet, testBase+"/audit", nil, admin, nil))

	seen := map[string]bool{}
	for _, entry := range audit.Entries {
		seen[entry.Action] = true
	}

	for _, action := range []string{"role.create", "role.update", "role.delete"} {
		if !seen[action] {
			t.Fatalf("audit missing %s: %+v", action, audit.Entries)
		}
	}
}

// TestCustomRolePermissions verifies a member holding a custom role is gated by
// its permission set rather than by one of the built-in role names.
func TestCustomRolePermissions(t *testing.T) {
	application := newTestApp(t)
	admin := login(t, application, "admin@example.com")

	origin := map[string]string{"Origin": "http://example.com"}

	create, _ := json.Marshal(contract.CreateRole{Name: "auditor_plus", Permissions: []string{permAuditRead, permRosterManage}})

	if created := do(t, application, http.MethodPost, testBase+"/roles", create, admin, origin); created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}

	invite, _ := json.Marshal(contract.InviteMember{Email: "plus@example.com", Role: "auditor_plus"})

	if invited := do(t, application, http.MethodPost, testBase+"/members", invite, admin, origin); invited.Code != http.StatusCreated {
		t.Fatalf("invite = %d: %s", invited.Code, invited.Body.String())
	}

	plus := login(t, application, "plus@example.com")

	if roster := do(t, application, http.MethodGet, testBase+"/members", nil, plus, nil); roster.Code != http.StatusOK {
		t.Fatalf("roster = %d: %s", roster.Code, roster.Body.String())
	}

	if audit := do(t, application, http.MethodGet, testBase+"/audit", nil, plus, nil); audit.Code != http.StatusOK {
		t.Fatalf("audit = %d: %s", audit.Code, audit.Body.String())
	}

	second, _ := json.Marshal(contract.CreateRole{Name: "nope", Permissions: []string{permReview}})

	if created := do(t, application, http.MethodPost, testBase+"/roles", second, plus, origin); created.Code != http.StatusForbidden {
		t.Fatalf("role manage without grant = %d: %s", created.Code, created.Body.String())
	}
}
