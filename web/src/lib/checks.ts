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
