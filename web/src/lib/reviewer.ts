// reviewerHref links any reviewer identifier (email or user id) to the profile page.
export function reviewerHref(key: string | null | undefined): string | null {
	const value = key?.trim() ?? ""
	return value === "" ? null : `/reviewers/${encodeURIComponent(value)}`
}

// makerHref links a maker email to the maker's submissions page.
export function makerHref(email: string | null | undefined): string | null {
	const value = email?.trim() ?? ""
	return value === "" ? null : `/makers/${encodeURIComponent(value)}`
}
