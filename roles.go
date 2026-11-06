package main

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// permissionCatalog lists every permission an admin can grant to a role. The
// wildcard is reserved for the locked admin role and is never grantable.
var permissionCatalog = []string{
	permReview,
	permSecondPass,
	permChecksManage,
	permViewPII,
	permNoteManage,
	permRosterManage,
	permAuditRead,
	permSessionRevoke,
	permRoleManage,
}

// defaultRolePermissions seeds the built-in roles. The sets mirror the role
// comparisons they replace, so the migration changes no behaviour.
func defaultRolePermissions() map[string][]string {
	return map[string][]string{
		roleAdmin:     {permWildcard},
		roleOrganizer: {permReview, permSecondPass, permChecksManage, permViewPII},
		roleReviewer:  {permReview},
		roleAuditor:   {permAuditRead, permViewPII},
	}
}

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

// roleDefinition is one programme role and the permissions it grants.
type roleDefinition struct {
	Name        string
	Permissions []string
	Locked      bool
}

// grantsWildcard reports whether the role allows everything.
func (d roleDefinition) grantsWildcard() bool {
	for _, permission := range d.Permissions {
		if permission == permWildcard {
			return true
		}
	}

	return false
}

// validPermission reports whether admins may grant the permission to a role.
func validPermission(permission string) bool {
	for _, candidate := range permissionCatalog {
		if candidate == permission {
			return true
		}
	}

	return false
}

// listRoleDefinitions reads the programme roles with locked roles first.
func (a *ireApp) listRoleDefinitions(ctx context.Context, programID string) ([]roleDefinition, error) {
	rows, err := a.store.Query(ctx, `select name, permissions, locked from role_definition
		where program_id = ? order by locked desc, name`, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	definitions := []roleDefinition{}

	for rows.Next() {
		var (
			definition roleDefinition
			raw        string
			locked     int
		)

		if err := rows.Scan(&definition.Name, &raw, &locked); err != nil {
			return nil, err
		}

		definition.Permissions = unmarshalStrings(raw)
		definition.Locked = locked == 1
		definitions = append(definitions, definition)
	}

	return definitions, rows.Err()
}

// roleDefinitionByName reads one programme role.
func (a *ireApp) roleDefinitionByName(ctx context.Context, programID, name string) (roleDefinition, bool, error) {
	var (
		definition roleDefinition
		raw        string
		locked     int
	)

	err := a.store.QueryRow(ctx, `select name, permissions, locked from role_definition
		where program_id = ? and name = ?`, programID, name).
		Scan(&definition.Name, &raw, &locked)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return roleDefinition{}, false, nil
		}

		return roleDefinition{}, false, err
	}

	definition.Permissions = unmarshalStrings(raw)
	definition.Locked = locked == 1

	return definition, true, nil
}

// validMemberRole reports whether the roster accepts the role.
func (a *ireApp) validMemberRole(ctx context.Context, programID, role string) bool {
	_, found, err := a.roleDefinitionByName(ctx, programID, role)

	return err == nil && found
}

// adminHolderCount reports how many active members hold a wildcard role.
func (a *ireApp) adminHolderCount(ctx context.Context, programID string) (int, error) {
	definitions, err := a.listRoleDefinitions(ctx, programID)
	if err != nil {
		return 0, err
	}

	wildcards := []any{programID}

	for _, definition := range definitions {
		if definition.grantsWildcard() {
			wildcards = append(wildcards, definition.Name)
		}
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(wildcards)-1), ", ")

	var count int

	err = a.store.QueryRow(ctx, `select count(*) from membership
		where program_id = ? and active = 1 and role in (`+placeholders+`)`, wildcards...).Scan(&count)

	return count, err
}

// requirePermission loads the signed-in reviewer and refuses anyone whose role lacks the permission.
func (a *ireApp) requirePermission(w http.ResponseWriter, r *http.Request, permission, message string) (reviewer, bool) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return reviewer{}, false
	}

	if !person.Has(permission) {
		app.WriteError(w, http.StatusForbidden, message)

		return reviewer{}, false
	}

	return person, true
}

// requireRosterManager allows members who may invite and update roster rows.
func (a *ireApp) requireRosterManager(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	return a.requirePermission(w, r, permRosterManage, "only admins can manage members")
}

// requireRoleManager allows members who may create roles and change role permissions.
func (a *ireApp) requireRoleManager(w http.ResponseWriter, r *http.Request) (reviewer, bool) {
	return a.requirePermission(w, r, permRoleManage, "only admins can manage roles")
}

// handleListRoles returns the programme role definitions and their permissions.
func (a *ireApp) handleListRoles(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireRosterManager(w, r)
	if !ok {
		return
	}

	definitions, err := a.listRoleDefinitions(r.Context(), person.ProgramID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	roles := make([]contract.RoleDefinition, 0, len(definitions))

	for _, definition := range definitions {
		roles = append(roles, contract.RoleDefinition{
			Locked:      definition.Locked,
			Name:        definition.Name,
			Permissions: definition.Permissions,
		})
	}

	app.WriteJSON(w, http.StatusOK, contract.RolesResponse{Roles: roles})
}

// handleCreateRole adds a programme role with an explicit permission set.
func (a *ireApp) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireRoleManager(w, r)
	if !ok {
		return
	}

	var request contract.CreateRole
	if !app.ReadJSON(w, r, maxAPIBody, "name", &request) {
		return
	}

	name := strings.ToLower(strings.TrimSpace(request.Name))

	if !roleNamePattern.MatchString(name) {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "name", "use 2-32 lowercase letters, numbers, dashes or underscores")
		return
	}

	permissions, ok := normalizePermissions(w, request.Permissions)
	if !ok {
		return
	}

	if _, found, err := a.roleDefinitionByName(r.Context(), person.ProgramID, name); err != nil {
		writeStorage(w, err)

		return
	} else if found {
		app.WriteErrorField(w, http.StatusConflict, "exists", "name", "a role with this name already exists")
		return
	}

	if _, err := a.store.Exec(r.Context(), `insert into role_definition (id, program_id, name, permissions, locked, created_at)
		values (?, ?, ?, ?, 0, ?)`, newID("rol"), person.ProgramID, name, marshalStrings(permissions), a.nowText()); err != nil {
		writeStorage(w, err)

		return
	}

	a.writeAudit(r.Context(), person, "role.create", "role", name,
		map[string]any{}, map[string]any{"permissions": permissions})

	app.WriteJSON(w, http.StatusCreated, contract.RoleDefinition{Name: name, Permissions: permissions})
}

// handleUpdateRole replaces a role's permission set. Locked roles always allow everything.
func (a *ireApp) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireRoleManager(w, r)
	if !ok {
		return
	}

	var request contract.UpdateRole
	if !app.ReadJSON(w, r, maxAPIBody, "permissions", &request) {
		return
	}

	permissions, ok := normalizePermissions(w, request.Permissions)
	if !ok {
		return
	}

	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))

	current, found, err := a.roleDefinitionByName(r.Context(), person.ProgramID, name)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found {
		app.WriteError(w, http.StatusNotFound, "unknown role")
		return
	}

	if current.Locked {
		app.WriteError(w, http.StatusForbidden, "the admin role always allows everything")
		return
	}

	if _, err := a.store.Exec(r.Context(), `update role_definition set permissions = ? where program_id = ? and name = ?`,
		marshalStrings(permissions), person.ProgramID, name); err != nil {
		writeStorage(w, err)

		return
	}

	a.writeAudit(r.Context(), person, "role.update", "role", name,
		map[string]any{"permissions": current.Permissions}, map[string]any{"permissions": permissions})

	app.WriteJSON(w, http.StatusOK, contract.RoleDefinition{Name: name, Permissions: permissions})
}

// handleDeleteRole removes a role that no roster row uses. Locked roles cannot be deleted.
func (a *ireApp) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireRoleManager(w, r)
	if !ok {
		return
	}

	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))

	current, found, err := a.roleDefinitionByName(r.Context(), person.ProgramID, name)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found {
		app.WriteError(w, http.StatusNotFound, "unknown role")
		return
	}

	if current.Locked {
		app.WriteError(w, http.StatusForbidden, "the admin role cannot be deleted")
		return
	}

	var assigned int

	if err := a.store.QueryRow(r.Context(), `select count(*) from membership where program_id = ? and role = ?`,
		person.ProgramID, name).Scan(&assigned); err != nil {
		writeStorage(w, err)

		return
	}

	if assigned > 0 {
		app.WriteError(w, http.StatusConflict, "move every member to another role first")
		return
	}

	if _, err := a.store.Exec(r.Context(), `delete from role_definition where program_id = ? and name = ?`,
		person.ProgramID, name); err != nil {
		writeStorage(w, err)

		return
	}

	a.writeAudit(r.Context(), person, "role.delete", "role", name,
		map[string]any{"permissions": current.Permissions}, map[string]any{})

	w.WriteHeader(http.StatusNoContent)
}

// normalizePermissions validates an admin-supplied permission set.
func normalizePermissions(w http.ResponseWriter, permissions []string) ([]string, bool) {
	seen := map[string]bool{}
	clean := []string{}

	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)

		if !validPermission(permission) {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "permissions", "unknown permission "+permission)
			return nil, false
		}

		if !seen[permission] {
			seen[permission] = true
			clean = append(clean, permission)
		}
	}

	return clean, true
}
