import type { Me } from "./api"

// permissions mirrors the backend catalog in app.go: roles grant these
// strings, and the locked admin role holds the wildcard for all of them.
export const permissionWildcard = "*"
export const permissionReview = "review"
export const permissionSecondPass = "second_pass"
export const permissionChecksManage = "checks_manage"
export const permissionViewPII = "view_pii"
export const permissionNoteManage = "note_manage"
export const permissionRosterManage = "roster_manage"
export const permissionAuditRead = "audit_read"
export const permissionSessionRevoke = "session_revoke"
export const permissionRoleManage = "role_manage"

export interface PermissionInfo {
	description: string
	label: string
	value: string
}

// permissionCatalog drives the admin role matrix, in the order it is shown.
export const permissionCatalog: PermissionInfo[] = [
	{ description: "Claim, draft and decide ships at stage 1.", label: "Review", value: permissionReview },
	{
		description: "Decide held ships, send them back and approve fraud checks.",
		label: "2nd pass",
		value: permissionSecondPass,
	},
	{
		description: "Rerun repository checks and refresh evidence snapshots.",
		label: "Checks",
		value: permissionChecksManage,
	},
	{ description: "Read unredacted delivery payloads.", label: "Unredacted data", value: permissionViewPII },
	{ description: "Remove notes written by someone else.", label: "Notes", value: permissionNoteManage },
	{ description: "Invite members and change roles.", label: "Roster", value: permissionRosterManage },
	{ description: "Read the audit log.", label: "Audit", value: permissionAuditRead },
	{ description: "Revoke another member's sessions.", label: "Sessions", value: permissionSessionRevoke },
	{
		description: "Create roles and change role permissions.",
		label: "Roles",
		value: permissionRoleManage,
	},
]

// hasPermission reports whether a membership permission set grants the permission.
export function hasPermission(permissions: string[], permission: string): boolean {
	return permissions.includes(permissionWildcard) || permissions.includes(permission)
}

// programPermissions returns the signed-in permission set for one programme slug.
export function programPermissions(me: Me | null, slug: string): string[] {
	return me?.programs.find(item => item.programSlug === slug)?.permissions ?? []
}
