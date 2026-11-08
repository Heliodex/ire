import { describe, expect, test } from "bun:test"
import { type ReviewPreview, readReviewPreview, rememberReviewPreview } from "../src/lib/review-preview"

function preview(id: string, overrides: Partial<ReviewPreview> = {}): ReviewPreview {
	return {
		bomTotalCents: 0,
		hoursClaimedSeconds: 0,
		hoursHackatimeSeconds: 0,
		hoursJournalSeconds: 0,
		id,
		makerName: "Maker",
		status: "ready",
		title: id,
		version: 1,
		...overrides,
	}
}

describe("review previews", () => {
	test("remembers the row a queue visit already loaded", () => {
		const item = preview("sub_preview_1", {
			makerEmail: "ada@example.com",
			makerSlackId: "U123",
			requestedTier: "2",
			week: 2,
		})

		rememberReviewPreview(item)

		expect(readReviewPreview("sub_preview_1")).toEqual(item)
		expect(readReviewPreview("sub_other")).toBeUndefined()
	})

	test("keeps the latest copy of a ship", () => {
		rememberReviewPreview(preview("sub_preview_2", { status: "ready", title: "Old" }))
		rememberReviewPreview(preview("sub_preview_2", { status: "claimed", title: "New" }))

		expect(readReviewPreview("sub_preview_2")?.title).toBe("New")
		expect(readReviewPreview("sub_preview_2")?.status).toBe("claimed")
	})
})
