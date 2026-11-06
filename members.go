package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// requireAdmin loads the signed-in reviewer and refuses anyone who cannot manage the roster.
func (a *ireApp) requireAdmin(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	return a.requireRosterManager(w, r)
}

// handleMembers lists the programme roster with roles and last sign-in.
func (a *ireApp) handleMembers(w http.ResponseWriter, r *http.Request) {
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
func (a *ireApp) handleInviteMember(w http.ResponseWriter, r *http.Request) {
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

	if !a.validMemberRole(r.Context(), person.ProgramID, role) {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "role", "unknown role")
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
func (a *ireApp) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	var request contract.UpdateMember
	if !app.ReadJSON(w, r, maxAPIBody, "role", &request) {
		return
	}

	role := strings.TrimSpace(request.Role)
	if !a.validMemberRole(r.Context(), person.ProgramID, role) {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "role", "unknown role")
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

	losingAdmin := current.Active && (role != current.Role || !request.Active)
	if losingAdmin {
		currentDef, _, err := a.roleDefinitionByName(r.Context(), person.ProgramID, current.Role)
		if err != nil {
			writeStorage(w, err)

			return
		}

		nextDef, _, err := a.roleDefinitionByName(r.Context(), person.ProgramID, role)
		if err != nil {
			writeStorage(w, err)

			return
		}

		if currentDef.grantsWildcard() && (!request.Active || !nextDef.grantsWildcard()) {
			admins, err := a.adminHolderCount(r.Context(), person.ProgramID)
			if err != nil {
				writeStorage(w, err)

				return
			}

			if admins <= 1 {
				app.WriteError(w, http.StatusConflict, "a programme needs at least one active admin")
				return
			}
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

// requireAuditReader allows members who may read the programme audit log.
func (a *ireApp) requireAuditReader(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	return a.requirePermission(w, r, permAuditRead, "only admins and auditors can read the audit log")
}

// handleAudit lists recent audit entries for the programme.
func (a *ireApp) handleAudit(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAuditReader(w, r)
	if !ok {
		return
	}

	limit := parseLimit(r)
	offset := parseOffset(r)

	conditions := []string{"a.program_id = ?"}
	args := []any{person.ProgramID}

	if action := strings.TrimSpace(r.URL.Query().Get("action")); action != "" {
		conditions = append(conditions, "a.action = ?")
		args = append(args, action)
	}

	if query := strings.TrimSpace(r.URL.Query().Get("q")); query != "" {
		like := "%" + strings.ToLower(query) + "%"

		conditions = append(conditions, `(lower(a.action) like ? or lower(coalesce(a.actor_email, '')) like ?
			or lower(coalesce(au.name, '')) like ? or lower(coalesce(a.target_type, '')) like ?
			or lower(coalesce(a.target_id, '')) like ? or lower(coalesce(tu.email, ts.title, '')) like ?
			or lower(coalesce(a.reason, '')) like ?)`)
		args = append(args, like, like, like, like, like, like, like)
	}

	if value := strings.TrimSpace(r.URL.Query().Get("days")); value != "" {
		days, err := strconv.Atoi(value)
		if err != nil || days < 1 || days > 3650 {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "days", "days must be between 1 and 3650")
			return
		}

		conditions = append(conditions, "a.created_at >= ?")
		args = append(args, a.now().AddDate(0, 0, -days).Format(timestampFormat))
	}

	query := `select a.id, coalesce(a.actor_email, ''), coalesce(au.name, ''), coalesce(au.avatar_url, ''),
			a.action, coalesce(a.target_type, ''), coalesce(a.target_id, ''), coalesce(tu.email, ts.title, ''),
			coalesce(a.before_json, ''), coalesce(a.after_json, ''), coalesce(a.reason, ''), a.created_at
		from audit_log a
		left join user au on au.id = a.actor_id
		left join user tu on a.target_type = 'user' and tu.id = a.target_id
		left join submission ts on a.target_type = 'submission' and ts.id = a.target_id
		where ` + strings.Join(conditions, " and ") + `
		order by a.created_at desc, a.id desc limit ? offset ?`

	args = append(args, limit+1, offset)

	rows, err := a.store.Query(r.Context(), query, args...)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.AuditResponse{Entries: []contract.AuditEntry{}}

	for rows.Next() {
		var entry contract.AuditEntry

		var actor, actorName, actorAvatar, targetType, targetID, targetLabel, before, after, reason string

		if err := rows.Scan(&entry.ID, &actor, &actorName, &actorAvatar, &entry.Action, &targetType, &targetID, &targetLabel,
			&before, &after, &reason, &entry.CreatedAt); err != nil {
			writeStorage(w, err)

			return
		}

		entry.ActorEmail = optionalText(actor)
		entry.ActorName = optionalText(actorName)
		entry.ActorAvatarURL = optionalText(actorAvatar)
		entry.TargetType = optionalText(targetType)
		entry.TargetID = optionalText(targetID)
		entry.TargetLabel = optionalText(targetLabel)
		entry.BeforeJson = optionalText(before)
		entry.AfterJson = optionalText(after)
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
func (a *ireApp) listMembers(ctx context.Context, programID string) ([]contract.Member, error) {
	rows, err := a.store.Query(ctx, `select u.id, u.email, coalesce(u.name, ''), coalesce(u.avatar_url, ''), m.role, m.active, m.created_at, coalesce(u.last_login_at, '')
		from membership m join user u on u.id = m.user_id
		where m.program_id = ? order by m.active desc, m.role, u.email`, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []contract.Member{}

	for rows.Next() {
		var member contract.Member

		var name, avatar, lastLogin string

		if err := rows.Scan(&member.ID, &member.Email, &name, &avatar, &member.Role, &member.Active, &member.CreatedAt, &lastLogin); err != nil {
			return nil, err
		}

		member.Name = optionalText(name)
		member.AvatarURL = optionalText(avatar)
		member.LastLoginAt = optionalText(lastLogin)
		members = append(members, member)
	}

	return members, rows.Err()
}

// memberByUserID reads one roster row.
func (a *ireApp) memberByUserID(ctx context.Context, programID, userID string) (contract.Member, bool, error) {
	var member contract.Member

	var name, avatar, lastLogin string

	err := a.store.QueryRow(ctx, `select u.id, u.email, coalesce(u.name, ''), coalesce(u.avatar_url, ''), m.role, m.active, m.created_at, coalesce(u.last_login_at, '')
		from membership m join user u on u.id = m.user_id
		where m.program_id = ? and m.user_id = ?`, programID, userID).
		Scan(&member.ID, &member.Email, &name, &avatar, &member.Role, &member.Active, &member.CreatedAt, &lastLogin)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return contract.Member{}, false, nil
		}

		return contract.Member{}, false, err
	}

	member.Name = optionalText(name)
	member.AvatarURL = optionalText(avatar)
	member.LastLoginAt = optionalText(lastLogin)

	return member, true, nil
}

// writeAudit stores one admin action with its before and after values.
func (a *ireApp) writeAudit(ctx context.Context, person reviewer, action, targetType, targetID string, before, after map[string]any) {
	_, _ = a.store.Exec(ctx, `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID("aud"), person.ProgramID, person.ID, person.Email, action, targetType, targetID,
		marshalMap(before), marshalMap(after), a.nowText())
}
