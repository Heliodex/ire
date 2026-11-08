// reviewerHref links any reviewer identifier (email or user id) to the profile page.
export function reviewerHref(key: string | null | undefined): string | undefined {
	const value = key?.trim() ?? ""
	return value === "" ? undefined : `/reviewers/${encodeURIComponent(value)}`
}

// makerHref links a maker email to the maker's submissions page.
export function makerHref(email: string | null | undefined): string | undefined {
	const value = email?.trim() ?? ""
	return value === "" ? undefined : `/makers/${encodeURIComponent(value)}`
}
