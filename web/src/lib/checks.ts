// CheckLike is the smallest shape shared by queue dots and the full review checks.
export interface CheckLike {
	severity: string
	status: string
}

// checkTone maps a check to a badge tone.
export function checkTone(check: CheckLike): string {
	if (check.status === "pass") return "approved"
	if (check.status === "warn") return "pending"
	if (check.status === "fail" && check.severity === "warn") return "changes"
	if (check.status === "fail" || check.status === "error") return "rejected"

	return "neutral"
}

// checkGroupLabel turns a stored group key into a readable label.
export function checkGroupLabel(group: string): string {
	switch (group) {
		case "repo":
			return "Repository"
		case "evidence":
			return "Evidence"
		case "demo":
			return "Demo"
		case "identity":
			return "Identity"
		case "duplicate":
			return "Duplicates"
		case "fraud":
			return "Fraud"
		default:
			return group
	}
}

// checkStatusLabel turns a check status into the wording shown in the tooltip.
export function checkStatusLabel(status: string): string {
	switch (status) {
		case "pass":
			return "Passed"
		case "warn":
			return "Warning"
		case "fail":
			return "Failed"
		case "error":
			return "Error"
		case "skip":
			return "Skipped"
		default:
			return status
	}
}

// isWarning reports a check the reviewer must acknowledge before approving.
export function isWarning(check: CheckLike): boolean {
	return check.status === "warn" || (check.status === "fail" && check.severity === "warn")
}

// isBlocking reports a check that disables approval.
export function isBlocking(check: CheckLike): boolean {
	return check.severity === "block" && (check.status === "fail" || check.status === "error")
}

// overallChecks summarises a set of checks.
export function overallChecks(checks: CheckLike[]): string {
	if (checks.some(isBlocking)) return "fail"
	if (checks.some(isWarning)) return "warn"

	return "pass"
}

// checkDotMode selects the dot cap from the viewport: full above 1400px, wide above 1280px, compact below.
export type CheckDotMode = "compact" | "wide" | "full"

export const maxCheckDots = 11
export const maxWideCheckDots = 20

// visibleChecks caps the dots so they never run into the age column, except in full mode where the column expands to fit them all.
export function visibleChecks<T extends CheckLike>(checks: T[], mode: CheckDotMode): T[] {
	if (mode === "full") return checks

	return checks.slice(0, mode === "wide" ? maxWideCheckDots : maxCheckDots)
}

// checkOverflow counts the dots hidden behind the +N badge.
export function checkOverflow(checks: CheckLike[], mode: CheckDotMode): number {
	return checks.length - visibleChecks(checks, mode).length
}

// groupChecks returns checks in the review's group order.
export function groupChecks<T extends { group: string }>(checks: T[]): { group: string; items: T[] }[] {
	const order = ["repo", "evidence", "demo", "identity", "duplicate", "fraud"]
	const map = new Map<string, T[]>()

	for (const check of checks) {
		const list = map.get(check.group) ?? []
		list.push(check)
		map.set(check.group, list)
	}

	return [...map.entries()]
		.toSorted(([a], [b]) => groupRank(order, a) - groupRank(order, b))
		.map(([group, items]) => ({ group, items }))
}

// groupRank places known groups first and unknown groups last.
function groupRank(order: string[], group: string): number {
	const index = order.indexOf(group)

	return index === -1 ? order.length : index
}
