import {
	array,
	integer,
	maxLength,
	maxValue,
	minLength,
	minValue,
	number,
	object,
	optional,
	pipe,
	regex,
	string,
	title,
} from "valibot"

// These schemas are the source of truth for the generated Go contract. Request schemas describe JSON bodies clients send; response schemas describe what every endpoint returns. The client parses every response, so a field the UI reads must exist here.

export const maxNote = 4000

const nonBlank = regex(/\S/, "Please write something.")
const nonNegative = pipe(number(), integer(), minValue(0))
const optionalString = optional(string())
const timestamp = pipe(string(), minLength(1))

const decisionValues = pipe(
	string(),
	regex(/^(approved|changes|rejected)$/, "Choose approved, changes or rejected."),
)

// ─── Shared ─────────────────────────────────────────────────────────────────

export const checkSummarySchema = pipe(
	object({
		overall: pipe(string(), minLength(1)),
		pass: nonNegative,
		warn: nonNegative,
		fail: nonNegative,
		error: nonNegative,
		running: nonNegative,
	}),
	title("CheckSummary"),
)

export const checkResultSchema = pipe(
	object({
		key: pipe(string(), minLength(1)),
		label: pipe(string(), minLength(1)),
		group: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
		severity: pipe(string(), minLength(1)),
		summary: pipe(string(), minLength(1)),
		detail: optionalString,
		checkedAt: timestamp,
	}),
	title("CheckResult"),
)

export const errorResponseSchema = pipe(
	object({ code: pipe(string(), minLength(1)), error: pipe(string(), minLength(1)), field: optional(string()) }),
	title("ErrorResponse"),
)

// ─── Auth ───────────────────────────────────────────────────────────────────

export const loginRequestSchema = pipe(
	object({ email: pipe(string(), minLength(1), maxLength(320), nonBlank) }),
	title("LoginRequest"),
)

export const membershipSchema = pipe(
	object({
		programId: pipe(string(), minLength(1)),
		programSlug: pipe(string(), minLength(1)),
		programName: pipe(string(), minLength(1)),
		role: pipe(string(), minLength(1)),
	}),
	title("Membership"),
)

export const meResponseSchema = pipe(
	object({
		user: object({
			id: pipe(string(), minLength(1)),
			email: pipe(string(), minLength(1)),
			name: optionalString,
		}),
		programs: array(membershipSchema),
	}),
	title("MeResponse"),
)

// ─── Queue ──────────────────────────────────────────────────────────────────

export const queueCountsSchema = pipe(
	object({
		ready: nonNegative,
		claimed: nonNegative,
		needsAttention: nonNegative,
		stage2Hold: nonNegative,
	}),
	title("QueueCounts"),
)

export const queueCheckSchema = pipe(
	object({
		key: pipe(string(), minLength(1)),
		label: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
		severity: pipe(string(), minLength(1)),
	}),
	title("QueueCheck"),
)

export const queueItemSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		externalId: pipe(string(), minLength(1)),
		version: nonNegative,
		status: pipe(string(), minLength(1)),
		stage: nonNegative,
		priority: nonNegative,
		title: pipe(string(), minLength(1)),
		theme: optionalString,
		thumbnailUrl: optionalString,
		week: optional(pipe(number(), integer())),
		makerName: pipe(string(), minLength(1)),
		hoursClaimedSeconds: nonNegative,
		checkSummary: checkSummarySchema,
		checks: array(queueCheckSchema),
		ingestedAt: timestamp,
		claimedBy: optionalString,
	}),
	title("QueueItem"),
)

export const queueResponseSchema = pipe(
	object({ counts: queueCountsSchema, items: array(queueItemSchema) }),
	title("QueueResponse"),
)

// ─── Review bundle ──────────────────────────────────────────────────────────

export const submissionSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		externalId: pipe(string(), minLength(1)),
		version: nonNegative,
		status: pipe(string(), minLength(1)),
		stage: nonNegative,
		title: pipe(string(), minLength(1)),
		description: optionalString,
		track: pipe(string(), minLength(1)),
		theme: optionalString,
		phase: optionalString,
		week: optional(pipe(number(), integer())),
		makerEmail: pipe(string(), minLength(1)),
		makerName: optionalString,
		repoUrl: optionalString,
		demoUrl: optionalString,
		thumbnailUrl: optionalString,
		requestedTier: optional(pipe(number(), integer())),
		grantCapCents: optional(pipe(number(), integer())),
		bomTotalCents: optional(pipe(number(), integer())),
		hoursClaimedSeconds: nonNegative,
		hoursJournalSeconds: nonNegative,
		hoursHackatimeSeconds: nonNegative,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		heldReason: optionalString,
		ingestedAt: timestamp,
		decidedAt: optionalString,
		updatedAt: timestamp,
	}),
	title("Submission"),
)

export const journalSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		at: timestamp,
		seconds: nonNegative,
		title: optionalString,
		text: optionalString,
		markdown: optionalString,
		mediaCount: nonNegative,
		clipCount: nonNegative,
		coveredSeconds: nonNegative,
	}),
	title("JournalEntry"),
)

export const bomLineSchema = pipe(
	object({
		name: pipe(string(), minLength(1)),
		purpose: optionalString,
		quantity: nonNegative,
		unitCostCents: nonNegative,
		vendor: optionalString,
		link: optionalString,
	}),
	title("BomLine"),
)

export const reviewSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		stage: nonNegative,
		decision: pipe(string(), minLength(1)),
		noteToMaker: string(),
		internalNote: optionalString,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		reviewerEmail: pipe(string(), minLength(1)),
		reviewerName: optionalString,
		held: nonNegative,
		createdAt: timestamp,
	}),
	title("Review"),
)

export const eventSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		kind: pipe(string(), minLength(1)),
		actorEmail: optionalString,
		data: optionalString,
		createdAt: timestamp,
	}),
	title("SubmissionEvent"),
)

export const priorReviewSchema = pipe(
	object({
		result: pipe(string(), minLength(1)),
		feedback: optionalString,
		reviewer: optionalString,
		at: optionalString,
		invalidated: optional(pipe(number(), integer())),
	}),
	title("PriorReview"),
)

export const reviewPolicySchema = pipe(
	object({
		holdOnWarn: pipe(number(), integer(), minValue(0), maxValue(1)),
		holdMinApprovedSeconds: nonNegative,
		holdTiers: array(pipe(number(), integer())),
		noteRequiredForChanges: pipe(number(), integer(), minValue(0), maxValue(1)),
		fraudMinStage: nonNegative,
	}),
	title("ReviewPolicy"),
)

export const claimSchema = pipe(
	object({
		reviewerEmail: pipe(string(), minLength(1)),
		reviewerName: optionalString,
		expiresAt: timestamp,
		isMine: pipe(number(), integer(), minValue(0), maxValue(1)),
	}),
	title("ClaimState"),
)

export const draftSchema = pipe(
	object({
		note: string(),
		internalNote: string(),
		updatedAt: timestamp,
	}),
	title("DraftState"),
)

export const reviewBundleSchema = pipe(
	object({
		submission: submissionSchema,
		checks: array(checkResultSchema),
		journals: array(journalSchema),
		bom: array(bomLineSchema),
		reviews: array(reviewSchema),
		events: array(eventSchema),
		priorReviews: array(priorReviewSchema),
		claim: optional(claimSchema),
		draft: optional(draftSchema),
		acknowledgements: array(string()),
		policy: reviewPolicySchema,
	}),
	title("ReviewBundle"),
)

// ─── Actions ────────────────────────────────────────────────────────────────

export const claimResponseSchema = pipe(object({ claim: claimSchema }), title("ClaimResponse"))

export const draftRequestSchema = pipe(
	object({
		note: pipe(string(), maxLength(maxNote)),
		internalNote: pipe(string(), maxLength(maxNote)),
		values: string(),
		checklist: string(),
		adjustments: string(),
	}),
	title("DraftRequest"),
)

export const draftResponseSchema = pipe(
	object({ saved: pipe(number(), integer(), minValue(0), maxValue(1)), updatedAt: timestamp }),
	title("DraftSaved"),
)

export const decisionRequestSchema = pipe(
	object({
		decision: decisionValues,
		version: nonNegative,
		noteToMaker: pipe(string(), maxLength(maxNote)),
		internalNote: pipe(string(), maxLength(maxNote)),
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		acknowledgements: array(string()),
		idempotencyKey: pipe(string(), minLength(1), maxLength(120)),
	}),
	title("DecisionRequest"),
)

export const decisionResultSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
		held: pipe(number(), integer(), minValue(0), maxValue(1)),
		heldReason: optionalString,
		outboxId: optionalString,
		deliveryStatus: optionalString,
		nextId: optionalString,
	}),
	title("DecisionOutcome"),
)

// ─── Ingestion ──────────────────────────────────────────────────────────────

export const ingestJournalSchema = pipe(
	object({
		at: pipe(string(), minLength(1)),
		minutes: optional(pipe(number(), integer())),
		seconds: optional(pipe(number(), integer())),
		text: pipe(string(), minLength(1)),
		markdown: optionalString,
		media: optional(array(object({ type: optionalString, url: pipe(string(), minLength(1)), alt: optionalString }))),
		timelapses: optional(
			array(
				object({
					provider: optionalString,
					externalId: optionalString,
					url: optionalString,
					thumbnailUrl: optionalString,
					coveredSeconds: optional(pipe(number(), integer())),
				}),
			),
		),
	}),
	title("IngestJournal"),
)

export const ingestBomLineSchema = pipe(
	object({
		name: pipe(string(), minLength(1)),
		qty: optional(pipe(number(), integer())),
		unit_usd: optional(number()),
		vendor: optionalString,
		link: optionalString,
	}),
	title("IngestBomLine"),
)

export const ingestHoursSchema = pipe(
	object({
		claimed_seconds: nonNegative,
		journal_seconds: nonNegative,
		hackatime_seconds: nonNegative,
		timelapse_seconds: optional(pipe(number(), integer())),
	}),
	title("IngestHours"),
)

export const ingestPriorReviewSchema = pipe(
	object({
		result: pipe(string(), minLength(1)),
		feedback: optionalString,
		reviewer: optionalString,
		at: optionalString,
		invalidated: optional(pipe(number(), integer())),
	}),
	title("IngestPriorReview"),
)

export const ingestMetaSchema = pipe(
	object({
		program: optionalString,
		phase: optionalString,
		theme: optionalString,
		week: optional(pipe(number(), integer())),
		requested_tier: optional(pipe(number(), integer())),
		priority: optional(pipe(number(), integer())),
		grant_cap_cents: optional(pipe(number(), integer())),
		submission_notes: optionalString,
		bom: optional(array(ingestBomLineSchema)),
		hours: optional(ingestHoursSchema),
		prior_reviews: optional(array(ingestPriorReviewSchema)),
	}),
	title("IngestMeta"),
)

export const ingestMakerSchema = pipe(
	object({
		email: pipe(string(), minLength(1)),
		name: pipe(string(), minLength(1)),
		slack_id: optionalString,
		hackatime_id: optionalString,
	}),
	title("IngestMaker"),
)

export const ingestRequestSchema = pipe(
	object({
		external_id: pipe(string(), minLength(1), maxLength(200)),
		ingest_version: optional(pipe(number(), integer())),
		title: pipe(string(), minLength(1), maxLength(200)),
		description: pipe(string(), minLength(1), maxLength(20000)),
		repo_url: optionalString,
		demo_url: optional(string()),
		thumbnail_url: optional(string()),
		track: optionalString,
		evidence: optional(array(string())),
		hackatime_projects: optional(array(string())),
		journals: optional(array(ingestJournalSchema)),
		maker: ingestMakerSchema,
		meta: optional(ingestMetaSchema),
	}),
	title("IngestRequest"),
)

export const ingestResponseSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
		version: nonNegative,
	}),
	title("IngestAccepted"),
)

export const withdrawRequestSchema = pipe(
	object({ external_id: pipe(string(), minLength(1)) }),
	title("WithdrawRequest"),
)

export const withdrawResponseSchema = pipe(
	object({ id: pipe(string(), minLength(1)), status: pipe(string(), minLength(1)) }),
	title("WithdrawOutcome"),
)

// ─── Ari-compatible status ──────────────────────────────────────────────────

const stagesSchema = array(
	object({
		stage: nonNegative,
		decision: pipe(string(), minLength(1)),
		reviewer: optionalString,
		at: timestamp,
	}),
)

const secondsBreakdownSchema = object({
	hackatime: nonNegative,
	journals: nonNegative,
	lapse: nonNegative,
	program: nonNegative,
})

const statusReviewSchema = object({
	approved_seconds: nonNegative,
	approved_minutes: nonNegative,
	approved_hours: number(),
	seconds_breakdown: secondsBreakdownSchema,
	note_to_maker: string(),
	audit_note: optionalString,
	reviewer: optional(object({ email: optionalString, slack_id: optionalString })),
	justification: optional(
		object({
			hackatime_projects: optionalString,
			technical_features: optionalString,
			deflation_reason: optionalString,
		}),
	),
})

const statusChecksSchema = object({
	overall: pipe(string(), minLength(1)),
	results: array(
		object({
			key: pipe(string(), minLength(1)),
			status: pipe(string(), minLength(1)),
			summary: pipe(string(), minLength(1)),
			checked_at: timestamp,
		}),
	),
})

export const statusResponseSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		external_id: pipe(string(), minLength(1)),
		version: nonNegative,
		phase: pipe(string(), minLength(1)),
		decision: optional(string()),
		review: optional(statusReviewSchema),
		stage: nonNegative,
		stage_history: stagesSchema,
		checks: statusChecksSchema,
		fraud: optional(object({ status: string(), provider: optionalString, score: optional(pipe(number(), integer())) })),
		timeline: array(object({ kind: pipe(string(), minLength(1)), at: timestamp })),
		updated_at: timestamp,
	}),
	title("StatusResponse"),
)

// ─── Recovery feeds ─────────────────────────────────────────────────────────

export const reviewEventSchema = pipe(
	object({
		event: pipe(string(), minLength(1)),
		id: pipe(string(), minLength(1)),
		external_id: pipe(string(), minLength(1)),
		decision: pipe(string(), minLength(1)),
		stage: nonNegative,
		stage_history: optional(
			array(object({ stage: nonNegative, decision: string(), reviewer: optionalString, at: timestamp })),
		),
		createdAt: timestamp,
		review: object({
			approved_seconds: nonNegative,
			approved_minutes: nonNegative,
			approved_hours: number(),
			seconds_breakdown: secondsBreakdownSchema,
			note_to_maker: string(),
			audit_note: optionalString,
			reviewer: optional(object({ email: optionalString, slack_id: optionalString })),
			justification: optional(
				object({
					hackatime_projects: optionalString,
					technical_features: optionalString,
					deflation_reason: optionalString,
				}),
			),
		}),
	}),
	title("ReviewEvent"),
)

export const reviewsResponseSchema = pipe(
	object({ reviews: array(reviewEventSchema), cursor: optionalString }),
	title("ReviewsResponse"),
)

export const eventsResponseSchema = pipe(
	object({ events: array(object({ id: string(), submissionId: string(), kind: string(), actorEmail: optionalString, data: optionalString, createdAt: timestamp })) }),
	title("EventsResponse"),
)

// ─── Delivery debug ─────────────────────────────────────────────────────────

export const outboxRowSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		submissionId: optionalString,
		event: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
		attempts: nonNegative,
		lastHttpStatus: optional(pipe(number(), integer())),
		lastError: optionalString,
		createdAt: timestamp,
		deliveredAt: optionalString,
		deliveryId: optionalString,
		payload: optionalString,
	}),
	title("OutboxRow"),
)

export const outboxResponseSchema = pipe(
	object({ deliveries: array(outboxRowSchema) }),
	title("OutboxResponse"),
)

export const fakeDeliverySchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		deliveryId: pipe(string(), minLength(1)),
		event: pipe(string(), minLength(1)),
		timestamp: pipe(string(), minLength(1)),
		body: pipe(string(), minLength(1)),
		signature: pipe(string(), minLength(1)),
		verified: pipe(number(), integer(), minValue(0), maxValue(1)),
		receivedAt: timestamp,
	}),
	title("FakeDelivery"),
)

export const fakeDeliveriesResponseSchema = pipe(
	object({ deliveries: array(fakeDeliverySchema) }),
	title("FakeDeliveriesResponse"),
)

// Registries of endpoint contracts. Add new request and response schemas here.
export const requestSchemas = Object.freeze([
	loginRequestSchema,
	draftRequestSchema,
	decisionRequestSchema,
	ingestRequestSchema,
	withdrawRequestSchema,
])

export const responseSchemas = Object.freeze([
	checkResultSchema,
	checkSummarySchema,
	claimResponseSchema,
	decisionResultSchema,
	draftResponseSchema,
	errorResponseSchema,
	eventsResponseSchema,
	fakeDeliveriesResponseSchema,
	ingestResponseSchema,
	meResponseSchema,
	outboxResponseSchema,
	queueResponseSchema,
	reviewBundleSchema,
	reviewEventSchema,
	reviewsResponseSchema,
	statusResponseSchema,
	withdrawResponseSchema,
])
