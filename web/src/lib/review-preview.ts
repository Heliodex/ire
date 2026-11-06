// ReviewPreview is the part of a row the queue or search already knows, so the review page can show it while the
// full bundle loads.
export interface ReviewPreview {
	claimedBy?: string
	hoursClaimedSeconds: number
	id: string
	makerEmail?: string
	makerName: string
	status: string
	theme?: string
	title: string
	version: number
	week?: number
}

const maxPreviews = 20
const previews = new Map<string, ReviewPreview>()

// rememberReviewPreview keeps the latest known row for a ship, bounded so a long queue visit cannot grow forever.
export function rememberReviewPreview(preview: ReviewPreview): void {
	previews.delete(preview.id)
	previews.set(preview.id, preview)

	if (previews.size <= maxPreviews) return

	const oldest = previews.keys().next().value
	if (oldest !== undefined) previews.delete(oldest)
}

// readReviewPreview returns the remembered row for a ship, if this visit already loaded it.
export function readReviewPreview(id: string): ReviewPreview | undefined {
	return previews.get(id)
}
