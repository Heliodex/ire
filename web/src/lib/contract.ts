import {
	array,
	boolean,
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

const decisionValues = pipe(string(), regex(/^(approved|changes|rejected)$/, "Choose approved, changes or rejected."))

// ─── Shared ─────────────────────────────────────────────────────────────────

export const checkSummarySchema = pipe(
	object({
		error: nonNegative,
		fail: nonNegative,
		overall: pipe(string(), minLength(1)),
		pass: nonNegative,
		running: nonNegative,
		warn: nonNegative,
	}),
	title("CheckSummary"),
)

export const checkResultSchema = pipe(
	object({
		checkedAt: timestamp,
		detail: optionalString,
		group: pipe(string(), minLength(1)),
		key: pipe(string(), minLength(1)),
		label: pipe(string(), minLength(1)),
		severity: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
		summary: pipe(string(), minLength(1)),
	}),
	title("CheckResult"),
)

export const errorResponseSchema = pipe(
	object({ code: pipe(string(), minLength(1)), error: pipe(string(), minLength(1)), field: optional(string()) }),
	title("ErrorResponse"),
)

// ─── Auth ───────────────────────────────────────────────────────────────────

export const authConfigSchema = pipe(
	object({
		dev: boolean(),
		hca: boolean(),
	}),
	title("AuthConfig"),
)

export const loginRequestSchema = pipe(
	object({ email: pipe(string(), minLength(1), maxLength(320), nonBlank) }),
	title("LoginRequest"),
)

export const membershipSchema = pipe(
	object({
		programId: pipe(string(), minLength(1)),
		programName: pipe(string(), minLength(1)),
		programSlug: pipe(string(), minLength(1)),
		role: pipe(string(), minLength(1)),
	}),
	title("Membership"),
)

export const meResponseSchema = pipe(
	object({
		programs: array(membershipSchema),
		user: object({
			email: pipe(string(), minLength(1)),
			id: pipe(string(), minLength(1)),
			name: optionalString,
		}),
	}),
	title("MeResponse"),
)

// ─── Queue ──────────────────────────────────────────────────────────────────

export const queueCountsSchema = pipe(
	object({
		claimed: nonNegative,
		needsAttention: nonNegative,
		ready: nonNegative,
		stage2Hold: nonNegative,
	}),
	title("QueueCounts"),
)

export const queueCheckSchema = pipe(
	object({
		key: pipe(string(), minLength(1)),
		label: pipe(string(), minLength(1)),
		severity: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
	}),
	title("QueueCheck"),
)

export const queueItemSchema = pipe(
	object({
		checkSummary: checkSummarySchema,
		checks: array(queueCheckSchema),
		claimedBy: optionalString,
		externalId: pipe(string(), minLength(1)),
		hoursClaimedSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		ingestedAt: timestamp,
		makerName: pipe(string(), minLength(1)),
		priority: nonNegative,
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
		theme: optionalString,
		thumbnailUrl: optionalString,
		title: pipe(string(), minLength(1)),
		version: nonNegative,
		week: optional(pipe(number(), integer())),
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
		bomTotalCents: optional(pipe(number(), integer())),
		decidedAt: optionalString,
		demoUrl: optionalString,
		description: optionalString,
		externalId: pipe(string(), minLength(1)),
		grantCapCents: optional(pipe(number(), integer())),
		heldReason: optionalString,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		hoursClaimedSeconds: nonNegative,
		hoursHackatimeSeconds: nonNegative,
		hoursJournalSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		ingestedAt: timestamp,
		makerEmail: pipe(string(), minLength(1)),
		makerName: optionalString,
		phase: optionalString,
		repoUrl: optionalString,
		requestedTier: optional(pipe(number(), integer())),
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
		theme: optionalString,
		thumbnailUrl: optionalString,
		title: pipe(string(), minLength(1)),
		track: pipe(string(), minLength(1)),
		updatedAt: timestamp,
		version: nonNegative,
		week: optional(pipe(number(), integer())),
	}),
	title("Submission"),
)

export const journalSchema = pipe(
	object({
		at: timestamp,
		clipCount: nonNegative,
		coveredSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		markdown: optionalString,
		mediaCount: nonNegative,
		seconds: nonNegative,
		text: optionalString,
		title: optionalString,
	}),
	title("JournalEntry"),
)

export const bomLineSchema = pipe(
	object({
		link: optionalString,
		name: pipe(string(), minLength(1)),
		purpose: optionalString,
		quantity: nonNegative,
		unitCostCents: nonNegative,
		vendor: optionalString,
	}),
	title("BomLine"),
)

export const reviewSchema = pipe(
	object({
		createdAt: timestamp,
		decision: pipe(string(), minLength(1)),
		held: nonNegative,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		id: pipe(string(), minLength(1)),
		internalNote: optionalString,
		noteToMaker: string(),
		reviewerEmail: pipe(string(), minLength(1)),
		reviewerName: optionalString,
		stage: nonNegative,
	}),
	title("Review"),
)

export const eventSchema = pipe(
	object({
		actorEmail: optionalString,
		createdAt: timestamp,
		data: optionalString,
		id: pipe(string(), minLength(1)),
		kind: pipe(string(), minLength(1)),
	}),
	title("SubmissionEvent"),
)

export const priorReviewSchema = pipe(
	object({
		at: optionalString,
		feedback: optionalString,
		invalidated: optional(pipe(number(), integer())),
		result: pipe(string(), minLength(1)),
		reviewer: optionalString,
	}),
	title("PriorReview"),
)

export const reviewPolicySchema = pipe(
	object({
		fraudMinStage: nonNegative,
		holdMinApprovedSeconds: nonNegative,
		holdOnWarn: pipe(number(), integer(), minValue(0), maxValue(1)),
		holdTiers: array(pipe(number(), integer())),
		noteRequiredForChanges: pipe(number(), integer(), minValue(0), maxValue(1)),
	}),
	title("ReviewPolicy"),
)

export const claimSchema = pipe(
	object({
		expiresAt: timestamp,
		isMine: pipe(number(), integer(), minValue(0), maxValue(1)),
		reviewerEmail: pipe(string(), minLength(1)),
		reviewerName: optionalString,
	}),
	title("ClaimState"),
)

export const draftSchema = pipe(
	object({
		internalNote: string(),
		note: string(),
		updatedAt: timestamp,
	}),
	title("DraftState"),
)

export const reviewBundleSchema = pipe(
	object({
		acknowledgements: array(string()),
		bom: array(bomLineSchema),
		checks: array(checkResultSchema),
		claim: optional(claimSchema),
		draft: optional(draftSchema),
		events: array(eventSchema),
		journals: array(journalSchema),
		policy: reviewPolicySchema,
		priorReviews: array(priorReviewSchema),
		reviews: array(reviewSchema),
		submission: submissionSchema,
	}),
	title("ReviewBundle"),
)

// ─── Actions ────────────────────────────────────────────────────────────────

export const claimResponseSchema = pipe(object({ claim: claimSchema }), title("ClaimResponse"))

export const draftRequestSchema = pipe(
	object({
		adjustments: string(),
		checklist: string(),
		internalNote: pipe(string(), maxLength(maxNote)),
		note: pipe(string(), maxLength(maxNote)),
		values: string(),
	}),
	title("DraftRequest"),
)

export const draftResponseSchema = pipe(
	object({ saved: pipe(number(), integer(), minValue(0), maxValue(1)), updatedAt: timestamp }),
	title("DraftSaved"),
)

export const decisionRequestSchema = pipe(
	object({
		acknowledgements: array(string()),
		decision: decisionValues,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		idempotencyKey: pipe(string(), minLength(1), maxLength(120)),
		internalNote: pipe(string(), maxLength(maxNote)),
		noteToMaker: pipe(string(), maxLength(maxNote)),
		version: nonNegative,
	}),
	title("DecisionRequest"),
)

export const decisionResultSchema = pipe(
	object({
		deliveryStatus: optionalString,
		held: pipe(number(), integer(), minValue(0), maxValue(1)),
		heldReason: optionalString,
		id: pipe(string(), minLength(1)),
		nextId: optionalString,
		outboxId: optionalString,
		status: pipe(string(), minLength(1)),
	}),
	title("DecisionOutcome"),
)

// ─── Ingestion ──────────────────────────────────────────────────────────────

export const ingestJournalSchema = pipe(
	object({
		at: pipe(string(), minLength(1)),
		markdown: optionalString,
		media: optional(
			array(object({ alt: optionalString, type: optionalString, url: pipe(string(), minLength(1)) })),
		),
		minutes: optional(pipe(number(), integer())),
		seconds: optional(pipe(number(), integer())),
		text: pipe(string(), minLength(1)),
		timelapses: optional(
			array(
				object({
					coveredSeconds: optional(pipe(number(), integer())),
					externalId: optionalString,
					provider: optionalString,
					thumbnailUrl: optionalString,
					url: optionalString,
				}),
			),
		),
	}),
	title("IngestJournal"),
)

export const ingestBomLineSchema = pipe(
	object({
		link: optionalString,
		name: pipe(string(), minLength(1)),
		qty: optional(pipe(number(), integer())),
		unit_usd: optional(number()),
		vendor: optionalString,
	}),
	title("IngestBomLine"),
)

export const ingestHoursSchema = pipe(
	object({
		claimed_seconds: nonNegative,
		hackatime_seconds: nonNegative,
		journal_seconds: nonNegative,
		timelapse_seconds: optional(pipe(number(), integer())),
	}),
	title("IngestHours"),
)

export const ingestPriorReviewSchema = pipe(
	object({
		at: optionalString,
		feedback: optionalString,
		invalidated: optional(pipe(number(), integer())),
		result: pipe(string(), minLength(1)),
		reviewer: optionalString,
	}),
	title("IngestPriorReview"),
)

export const ingestMetaSchema = pipe(
	object({
		bom: optional(array(ingestBomLineSchema)),
		grant_cap_cents: optional(pipe(number(), integer())),
		hours: optional(ingestHoursSchema),
		phase: optionalString,
		prior_reviews: optional(array(ingestPriorReviewSchema)),
		priority: optional(pipe(number(), integer())),
		program: optionalString,
		requested_tier: optional(pipe(number(), integer())),
		submission_notes: optionalString,
		theme: optionalString,
		week: optional(pipe(number(), integer())),
	}),
	title("IngestMeta"),
)

export const ingestMakerSchema = pipe(
	object({
		email: pipe(string(), minLength(1)),
		hackatime_id: optionalString,
		name: pipe(string(), minLength(1)),
		slack_id: optionalString,
	}),
	title("IngestMaker"),
)

export const ingestRequestSchema = pipe(
	object({
		demo_url: optional(string()),
		description: pipe(string(), minLength(1), maxLength(20000)),
		evidence: optional(array(string())),
		external_id: pipe(string(), minLength(1), maxLength(200)),
		hackatime_projects: optional(array(string())),
		ingest_version: optional(pipe(number(), integer())),
		journals: optional(array(ingestJournalSchema)),
		maker: ingestMakerSchema,
		meta: optional(ingestMetaSchema),
		repo_url: optionalString,
		thumbnail_url: optional(string()),
		title: pipe(string(), minLength(1), maxLength(200)),
		track: optionalString,
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
		at: timestamp,
		decision: pipe(string(), minLength(1)),
		reviewer: optionalString,
		stage: nonNegative,
	}),
)

const secondsBreakdownSchema = object({
	hackatime: nonNegative,
	journals: nonNegative,
	lapse: nonNegative,
	program: nonNegative,
})

const statusReviewSchema = object({
	approved_hours: number(),
	approved_minutes: nonNegative,
	approved_seconds: nonNegative,
	audit_note: optionalString,
	justification: optional(
		object({
			deflation_reason: optionalString,
			hackatime_projects: optionalString,
			technical_features: optionalString,
		}),
	),
	note_to_maker: string(),
	reviewer: optional(object({ email: optionalString, slack_id: optionalString })),
	seconds_breakdown: secondsBreakdownSchema,
})

const statusChecksSchema = object({
	overall: pipe(string(), minLength(1)),
	results: array(
		object({
			checked_at: timestamp,
			key: pipe(string(), minLength(1)),
			status: pipe(string(), minLength(1)),
			summary: pipe(string(), minLength(1)),
		}),
	),
})

export const statusResponseSchema = pipe(
	object({
		checks: statusChecksSchema,
		decision: optional(string()),
		external_id: pipe(string(), minLength(1)),
		fraud: optional(
			object({ provider: optionalString, score: optional(pipe(number(), integer())), status: string() }),
		),
		id: pipe(string(), minLength(1)),
		phase: pipe(string(), minLength(1)),
		review: optional(statusReviewSchema),
		stage: nonNegative,
		stage_history: stagesSchema,
		timeline: array(object({ at: timestamp, kind: pipe(string(), minLength(1)) })),
		updated_at: timestamp,
		version: nonNegative,
	}),
	title("StatusResponse"),
)

// ─── Recovery feeds ─────────────────────────────────────────────────────────

export const reviewEventSchema = pipe(
	object({
		createdAt: timestamp,
		decision: pipe(string(), minLength(1)),
		event: pipe(string(), minLength(1)),
		external_id: pipe(string(), minLength(1)),
		id: pipe(string(), minLength(1)),
		review: object({
			approved_hours: number(),
			approved_minutes: nonNegative,
			approved_seconds: nonNegative,
			audit_note: optionalString,
			justification: optional(
				object({
					deflation_reason: optionalString,
					hackatime_projects: optionalString,
					technical_features: optionalString,
				}),
			),
			note_to_maker: string(),
			reviewer: optional(object({ email: optionalString, slack_id: optionalString })),
			seconds_breakdown: secondsBreakdownSchema,
		}),
		stage: nonNegative,
		stage_history: optional(
			array(object({ at: timestamp, decision: string(), reviewer: optionalString, stage: nonNegative })),
		),
	}),
	title("ReviewEvent"),
)

export const reviewsResponseSchema = pipe(
	object({ cursor: optionalString, reviews: array(reviewEventSchema) }),
	title("ReviewsResponse"),
)

export const eventsResponseSchema = pipe(
	object({
		events: array(
			object({
				actorEmail: optionalString,
				createdAt: timestamp,
				data: optionalString,
				id: string(),
				kind: string(),
				submissionId: string(),
			}),
		),
	}),
	title("EventsResponse"),
)

// ─── Delivery debug ─────────────────────────────────────────────────────────

export const outboxRowSchema = pipe(
	object({
		attempts: nonNegative,
		createdAt: timestamp,
		deliveredAt: optionalString,
		deliveryId: optionalString,
		event: pipe(string(), minLength(1)),
		id: pipe(string(), minLength(1)),
		lastError: optionalString,
		lastHttpStatus: optional(pipe(number(), integer())),
		payload: optionalString,
		status: pipe(string(), minLength(1)),
		submissionId: optionalString,
	}),
	title("OutboxRow"),
)

export const outboxResponseSchema = pipe(object({ deliveries: array(outboxRowSchema) }), title("OutboxResponse"))

export const fakeDeliverySchema = pipe(
	object({
		body: pipe(string(), minLength(1)),
		deliveryId: pipe(string(), minLength(1)),
		event: pipe(string(), minLength(1)),
		id: pipe(string(), minLength(1)),
		receivedAt: timestamp,
		signature: pipe(string(), minLength(1)),
		timestamp: pipe(string(), minLength(1)),
		verified: pipe(number(), integer(), minValue(0), maxValue(1)),
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
	authConfigSchema,
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
