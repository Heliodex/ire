package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// requireAdmin loads the signed-in reviewer and refuses anyone who is not an admin of the programme.
func (a *reviewApp) requireAdmin(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return reviewer{}, false
	}

	if person.Role != roleAdmin {
		app.WriteError(w, http.StatusForbidden, "only admins can manage members")
		return reviewer{}, false
	}

	return person, true
}

// handleMembers lists the programme roster with roles and last sign-in.
func (a *reviewApp) handleMembers(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	members, err := a.listMembers(r.Context(), person.ProgramID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.MembersResponse{Members: members})
}

// handleInviteMember adds a user to the roster, creating the user row when the email is new.
func (a *reviewApp) handleInviteMember(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	var request contract.InviteMember
	if !app.ReadJSON(w, r, maxAPIBody, "email", &request) {
		return
	}

	email := strings.ToLower(strings.TrimSpace(request.Email))
	role := strings.TrimSpace(request.Role)

	if !validMemberRole(role) {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "role", "choose reviewer, organizer, admin or auditor")
		return
	}

	var userID string

	err := a.store.QueryRow(r.Context(), `select id from user where email = ?`, email).Scan(&userID)
	if err != nil {
		if !errors.Is(err, storage.ErrNoRows) {
			writeStorage(w, err)

			return
		}

		userID = newID("usr")

		if _, err := a.store.Exec(r.Context(), `insert into user (id, email, created_at) values (?, ?, ?)`, userID, email, a.nowText()); err != nil {
			writeStorage(w, err)

			return
		}
	}

	now := a.nowText()

	if _, err := a.store.Exec(r.Context(), `insert into membership (id, program_id, user_id, role, tracks, permissions, active, created_at)
		values (?, ?, ?, ?, '["hardware"]', '[]', 1, ?)
		on conflict(program_id, user_id) do update set role = excluded.role, active = 1`,
		newID("mem"), person.ProgramID, userID, role, now); err != nil {
		writeStorage(w, err)

		return
	}

	a.writeAudit(r.Context(), person, "member.invite", "user", userID, map[string]any{}, map[string]any{"role": role, "email": email})

	member, found, err := a.memberByUserID(r.Context(), person.ProgramID, userID)
	if err != nil || !found {
		app.WriteError(w, http.StatusInternalServerError, "member was invited but could not be read back")

		return
	}

	app.WriteJSON(w, http.StatusCreated, member)
}

// handleUpdateMember changes a member's role or active flag and refuses to remove the last admin.
func (a *reviewApp) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	var request contract.UpdateMember
	if !app.ReadJSON(w, r, maxAPIBody, "role", &request) {
		return
	}

	role := strings.TrimSpace(request.Role)
	if !validMemberRole(role) {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "role", "choose reviewer, organizer, admin or auditor")
		return
	}

	target := r.PathValue("id")

	current, found, err := a.memberByUserID(r.Context(), person.ProgramID, target)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found {
		app.WriteError(w, http.StatusNotFound, "unknown member")
		return
	}

	losingAdmin := current.Role == roleAdmin && current.Active && (role != roleAdmin || !request.Active)
	if losingAdmin {
		admins, err := a.activeAdminCount(r.Context(), person.ProgramID)
		if err != nil {
			writeStorage(w, err)

			return
		}

		if admins <= 1 {
			app.WriteError(w, http.StatusConflict, "a programme needs at least one active admin")
			return
		}
	}

	if _, err := a.store.Exec(r.Context(), `update membership set role = ?, active = ? where program_id = ? and user_id = ?`,
		role, boolInt(request.Active), person.ProgramID, target); err != nil {
		writeStorage(w, err)

		return
	}

	a.writeAudit(r.Context(), person, "member.update", "user", target,
		map[string]any{"role": current.Role, "active": current.Active},
		map[string]any{"role": role, "active": request.Active})

	updated, found, err := a.memberByUserID(r.Context(), person.ProgramID, target)
	if err != nil || !found {
		app.WriteError(w, http.StatusInternalServerError, "member was updated but could not be read back")

		return
	}

	app.WriteJSON(w, http.StatusOK, updated)
}

// handleAudit lists recent audit entries for the programme.
func (a *reviewApp) handleAudit(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	limit := parseLimit(r)
	offset := parseOffset(r)

	rows, err := a.store.Query(r.Context(), `select id, coalesce(actor_email, ''), action, coalesce(target_type, ''),
			coalesce(target_id, ''), coalesce(reason, ''), created_at
		from audit_log where program_id = ? order by created_at desc, id desc limit ? offset ?`,
		person.ProgramID, limit+1, offset)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.AuditResponse{Entries: []contract.AuditEntry{}}

	for rows.Next() {
		var entry contract.AuditEntry

		var actor, targetType, targetID, reason string

		if err := rows.Scan(&entry.ID, &actor, &entry.Action, &targetType, &targetID, &reason, &entry.CreatedAt); err != nil {
			writeStorage(w, err)

			return
		}

		entry.ActorEmail = optionalText(actor)
		entry.TargetType = optionalText(targetType)
		entry.TargetID = optionalText(targetID)
		entry.Reason = optionalText(reason)
		response.Entries = append(response.Entries, entry)
	}

	if err := rows.Err(); err != nil {
		writeStorage(w, err)

		return
	}

	if len(response.Entries) > limit {
		response.Entries = response.Entries[:limit]
		response.HasMore = true
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// listMembers reads the roster in a stable order.
func (a *reviewApp) listMembers(ctx context.Context, programID string) ([]contract.Member, error) {
	rows, err := a.store.Query(ctx, `select u.id, u.email, coalesce(u.name, ''), m.role, m.active, m.created_at, coalesce(u.last_login_at, '')
		from membership m join user u on u.id = m.user_id
		where m.program_id = ? order by m.active desc, m.role, u.email`, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []contract.Member{}

	for rows.Next() {
		var member contract.Member

		var name, lastLogin string

		if err := rows.Scan(&member.ID, &member.Email, &name, &member.Role, &member.Active, &member.CreatedAt, &lastLogin); err != nil {
			return nil, err
		}

		member.Name = optionalText(name)
		member.LastLoginAt = optionalText(lastLogin)
		members = append(members, member)
	}

	return members, rows.Err()
}

// memberByUserID reads one roster row.
func (a *reviewApp) memberByUserID(ctx context.Context, programID, userID string) (contract.Member, bool, error) {
	var member contract.Member

	var name, lastLogin string

	err := a.store.QueryRow(ctx, `select u.id, u.email, coalesce(u.name, ''), m.role, m.active, m.created_at, coalesce(u.last_login_at, '')
		from membership m join user u on u.id = m.user_id
		where m.program_id = ? and m.user_id = ?`, programID, userID).
		Scan(&member.ID, &member.Email, &name, &member.Role, &member.Active, &member.CreatedAt, &lastLogin)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return contract.Member{}, false, nil
		}

		return contract.Member{}, false, err
	}

	member.Name = optionalText(name)
	member.LastLoginAt = optionalText(lastLogin)

	return member, true, nil
}

// activeAdminCount reports how many admins could still manage the programme.
func (a *reviewApp) activeAdminCount(ctx context.Context, programID string) (int, error) {
	var count int

	err := a.store.QueryRow(ctx, `select count(*) from membership where program_id = ? and role = ? and active = 1`,
		programID, roleAdmin).Scan(&count)

	return count, err
}

// validMemberRole reports whether the roster accepts the role.
func validMemberRole(role string) bool {
	switch role {
	case roleReviewer, roleOrganizer, roleAdmin, roleAuditor:
		return true
	}

	return false
}

// writeAudit stores one admin action with its before and after values.
func (a *reviewApp) writeAudit(ctx context.Context, person reviewer, action, targetType, targetID string, before, after map[string]any) {
	_, _ = a.store.Exec(ctx, `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID("aud"), person.ProgramID, person.ID, person.Email, action, targetType, targetID,
		marshalMap(before), marshalMap(after), a.nowText())
}
