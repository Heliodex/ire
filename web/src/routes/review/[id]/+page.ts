// Review ids are dynamic, but the static build needs one entry so the review shell exists as a page.
// The server serves that shell for every /review/<id> request and the router hydrates the real id.
export const prerender = true

export function entries(): { id: string }[] {
	return [{ id: "loading" }]
}
