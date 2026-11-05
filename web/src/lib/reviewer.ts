// reviewerHref links any reviewer identifier (email or user id) to the profile page.
export function reviewerHref(key: string | null | undefined): string | null {
	const value = key?.trim() ?? ""
	return value === "" ? null : `/reviewers/${encodeURIComponent(value)}`
}
