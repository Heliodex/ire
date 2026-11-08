import { ApiError } from "./api-error"
import { formatDateTime } from "./format"

// claimError makes a claim conflict readable and formats the expiry from the API.
export function claimError(err: unknown): string {
	if (!(err instanceof ApiError) || err.code !== "claim_taken")
		return err instanceof Error ? err.message : "Could not claim this ship."

	const until = /until (\S+)$/.exec(err.message)?.[1]

	return until !== undefined
		? `Another reviewer has already claimed this ship until ${formatDateTime(until)}.`
		: "Another reviewer has already claimed this ship."
}

// ageTone marks ships that have waited longer than the queue target.
export function ageTone(ingestedAt: string): string {
	const days = (Date.now() - Date.parse(ingestedAt)) / 86_400_000
	if (days >= 7) return "age--late"
	if (days >= 3) return "age--warn"
	return ""
}
