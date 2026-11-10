/* oxlint-disable max-lines */
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
const findingSeverityValues = pipe(
	string(),
	regex(/^(block|fixable|note|hold)$/, "Choose block, fixable, note or hold."),
)
const findingStatusValues = pipe(
	string(),
	regex(/^(open|fixed|accepted|disputed|withdrawn)$/, "Choose open, fixed, accepted, disputed or withdrawn."),
)

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
		source: optionalString,
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
		permissions: array(string()),
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
		readOnly: boolean(),
		user: object({
			avatarUrl: optionalString,
			email: pipe(string(), minLength(1)),
			id: pipe(string(), minLength(1)),
			name: optionalString,
		}),
	}),
	title("MeResponse"),
)

// ─── Admin ──────────────────────────────────────────────────────────────────

// memberRole accepts any non-empty role name. The server validates it against
// the programme role definitions, which admins can extend with custom roles.
const memberRole = pipe(string(), minLength(1))

export const memberSchema = pipe(
	object({
		active: boolean(),
		avatarUrl: optionalString,
		createdAt: timestamp,
		email: pipe(string(), minLength(1)),
		id: pipe(string(), minLength(1)),
		lastLoginAt: optionalString,
		name: optionalString,
		role: memberRole,
	}),
	title("Member"),
)

export const membersResponseSchema = pipe(object({ members: array(memberSchema) }), title("MembersResponse"))

export const reviewerReviewSchema = pipe(
	object({
		createdAt: timestamp,
		decision: pipe(string(), minLength(1)),
		held: nonNegative,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		id: pipe(string(), minLength(1)),
		invalidatedAt: optionalString,
		stage: nonNegative,
		submissionId: pipe(string(), minLength(1)),
		submissionTitle: pipe(string(), minLength(1)),
	}),
	title("ReviewerReview"),
)

export const reviewerStatsSchema = pipe(
	object({
		approved: nonNegative,
		changes: nonNegative,
		firstPass: nonNegative,
		held: nonNegative,
		hoursApprovedSeconds: nonNegative,
		invalidated: nonNegative,
		lastReviewAt: optionalString,
		rejected: nonNegative,
		secondPass: nonNegative,
		total: nonNegative,
	}),
	title("ReviewerStats"),
)

export const reviewerProfileSchema = pipe(
	object({
		active: optional(boolean()),
		avatarUrl: optionalString,
		email: pipe(string(), minLength(1)),
		id: optionalString,
		joinedAt: optionalString,
		lastLoginAt: optionalString,
		name: optionalString,
		permissions: array(string()),
		reviews: array(reviewerReviewSchema),
		reviewTotal: nonNegative,
		role: optionalString,
		stats: reviewerStatsSchema,
		tracks: array(string()),
	}),
	title("ReviewerProfile"),
)

export const makerSubmissionSchema = pipe(
	object({
		decidedAt: optionalString,
		decision: optionalString,
		externalId: pipe(string(), minLength(1)),
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		hoursClaimedSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
		theme: optionalString,
		thumbnailUrl: optionalString,
		title: pipe(string(), minLength(1)),
		updatedAt: timestamp,
		version: nonNegative,
	}),
	title("MakerSubmission"),
)

export const makerProfileSchema = pipe(
	object({
		avatarUrl: optionalString,
		email: pipe(string(), minLength(1)),
		name: optionalString,
		submissions: array(makerSubmissionSchema),
	}),
	title("MakerProfile"),
)

export const inviteMemberSchema = pipe(
	object({
		email: pipe(string(), minLength(3), maxLength(320), nonBlank),
		role: memberRole,
	}),
	title("InviteMember"),
)

export const updateMemberSchema = pipe(
	object({
		active: boolean(),
		role: memberRole,
	}),
	title("UpdateMember"),
)

export const roleDefinitionSchema = pipe(
	object({
		locked: boolean(),
		name: pipe(string(), minLength(1)),
		permissions: array(string()),
	}),
	title("RoleDefinition"),
)

export const rolesResponseSchema = pipe(object({ roles: array(roleDefinitionSchema) }), title("RolesResponse"))

export const createRoleSchema = pipe(
	object({
		name: pipe(string(), minLength(1)),
		permissions: array(string()),
	}),
	title("CreateRole"),
)

export const updateRoleSchema = pipe(
	object({
		permissions: array(string()),
	}),
	title("UpdateRole"),
)

export const auditEntrySchema = pipe(
	object({
		action: pipe(string(), minLength(1)),
		actorAvatarUrl: optionalString,
		actorEmail: optionalString,
		actorName: optionalString,
		afterJson: optionalString,
		beforeJson: optionalString,
		createdAt: timestamp,
		id: pipe(string(), minLength(1)),
		reason: optionalString,
		targetId: optionalString,
		targetLabel: optionalString,
		targetType: optionalString,
	}),
	title("AuditEntry"),
)

export const auditResponseSchema = pipe(
	object({ entries: array(auditEntrySchema), hasMore: boolean() }),
	title("AuditResponse"),
)

// ─── Queue ──────────────────────────────────────────────────────────────────

export const checkRunSchema = pipe(
	object({
		attempts: nonNegative,
		error: optionalString,
		finishedAt: optionalString,
		runnerVersion: pipe(string(), minLength(1)),
		startedAt: timestamp,
		status: pipe(string(), minLength(1)),
	}),
	title("CheckRun"),
)

export const checkHeadSchema = pipe(
	object({
		headSha: optionalString,
		outdated: boolean(),
		snapshotSha: optionalString,
	}),
	title("CheckHead"),
)

export const queueCountsSchema = pipe(
	object({
		checking: nonNegative,
		claimed: nonNegative,
		easy: nonNegative,
		fast: nonNegative,
		hard: nonNegative,
		medium: nonNegative,
		mine: nonNegative,
		needsAttention: nonNegative,
		ready: nonNegative,
		stage2Hold: nonNegative,
	}),
	title("QueueCounts"),
)

export const queueCheckSchema = pipe(
	object({
		group: pipe(string(), minLength(1)),
		key: pipe(string(), minLength(1)),
		label: pipe(string(), minLength(1)),
		severity: pipe(string(), minLength(1)),
		status: pipe(string(), minLength(1)),
	}),
	title("QueueCheck"),
)

export const queueItemSchema = pipe(
	object({
		bomTotalCents: nonNegative,
		checkRun: optional(checkRunSchema),
		checkSummary: checkSummarySchema,
		checks: array(queueCheckSchema),
		claimedBy: optionalString,
		externalId: pipe(string(), minLength(1)),
		hoursClaimedSeconds: nonNegative,
		hoursHackatimeSeconds: nonNegative,
		hoursJournalSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		ingestedAt: timestamp,
		journalCount: nonNegative,
		journalImageCount: nonNegative,
		journalTextLength: nonNegative,
		makerAvatarUrl: optionalString,
		makerEmail: pipe(string(), minLength(1)),
		makerName: pipe(string(), minLength(1)),
		makerSlackId: optionalString,
		previousReviewer: optionalString,
		priority: nonNegative,
		requestedTier: optionalString,
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
		submittedAt: timestamp,
		theme: optionalString,
		thumbnailUrl: optionalString,
		title: pipe(string(), minLength(1)),
		version: nonNegative,
		week: optional(pipe(number(), integer())),
	}),
	title("QueueItem"),
)

export const queueResponseSchema = pipe(
	object({ counts: queueCountsSchema, hasMore: boolean(), items: array(queueItemSchema) }),
	title("QueueResponse"),
)

// ─── Overview ───────────────────────────────────────────────────────────────

export const overviewWaitSchema = pipe(
	object({
		count: nonNegative,
		maxMs: nonNegative,
		oldestId: optionalString,
		oldestTitle: optionalString,
		p50Ms: nonNegative,
		p95Ms: nonNegative,
	}),
	title("OverviewWait"),
)

export const overviewStatusSchema = pipe(
	object({ count: nonNegative, status: pipe(string(), minLength(1)) }),
	title("OverviewStatus"),
)

export const overviewLeaderSchema = pipe(
	object({
		count: nonNegative,
		email: optionalString,
		name: optionalString,
	}),
	title("OverviewLeader"),
)

export const overviewDaySchema = pipe(
	object({
		date: pipe(string(), minLength(1)),
		decided: nonNegative,
		ingested: nonNegative,
	}),
	title("OverviewDay"),
)

export const overviewOutcomeSchema = pipe(
	object({ count: nonNegative, decision: decisionValues }),
	title("OverviewOutcome"),
)

export const overviewTurnaroundSchema = pipe(
	object({
		decisions: nonNegative,
		medianDecisionMs: nonNegative,
		medianFirstMs: nonNegative,
	}),
	title("OverviewTurnaround"),
)

export const overviewResponseSchema = pipe(
	object({
		byStatus: array(overviewStatusSchema),
		daily: array(overviewDaySchema),
		leaderboard: array(overviewLeaderSchema),
		needsReview: nonNegative,
		newToday: nonNegative,
		outcomes: array(overviewOutcomeSchema),
		reviewedToday: nonNegative,
		turnaround: overviewTurnaroundSchema,
		wait: overviewWaitSchema,
	}),
	title("OverviewResponse"),
)

// ─── Search ─────────────────────────────────────────────────────────────────

export const searchResultSchema = pipe(
	object({
		bomTotalCents: nonNegative,
		claimedBy: optionalString,
		externalId: pipe(string(), minLength(1)),
		hoursClaimedSeconds: nonNegative,
		hoursHackatimeSeconds: nonNegative,
		hoursJournalSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		ingestedAt: timestamp,
		makerEmail: optionalString,
		makerName: pipe(string(), minLength(1)),
		makerSlackId: optionalString,
		requestedTier: optionalString,
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
		theme: optionalString,
		thumbnailUrl: optionalString,
		title: pipe(string(), minLength(1)),
		version: nonNegative,
		week: optional(pipe(number(), integer())),
	}),
	title("SearchResult"),
)

export const searchResponseSchema = pipe(object({ results: array(searchResultSchema) }), title("SearchResponse"))

// ─── Reviewed ───────────────────────────────────────────────────────────────

export const reviewedItemSchema = pipe(
	object({
		decidedAt: timestamp,
		decision: decisionValues,
		externalId: pipe(string(), minLength(1)),
		hoursApprovedSeconds: nonNegative,
		hoursClaimedSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		makerName: pipe(string(), minLength(1)),
		stage: nonNegative,
		theme: optionalString,
		thumbnailUrl: optionalString,
		title: pipe(string(), minLength(1)),
	}),
	title("ReviewedItem"),
)

export const reviewedResponseSchema = pipe(
	object({ hasMore: boolean(), items: array(reviewedItemSchema) }),
	title("ReviewedResponse"),
)

// ─── Parity ─────────────────────────────────────────────────────────────────

export const parityShipSchema = pipe(
	object({
		bomTotalCents: nonNegative,
		checkSummary: checkSummarySchema,
		externalId: pipe(string(), minLength(1)),
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		hoursClaimedSeconds: nonNegative,
		id: pipe(string(), minLength(1)),
		phase: pipe(string(), minLength(1)),
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
		updatedAt: timestamp,
		version: nonNegative,
	}),
	title("ParityShip"),
)

export const parityResponseSchema = pipe(
	object({ generatedAt: timestamp, ships: array(parityShipSchema) }),
	title("ParityResponse"),
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
		makerSlackId: optionalString,
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

export const journalMediaSchema = pipe(
	object({
		alt: optionalString,
		url: pipe(string(), minLength(1)),
	}),
	title("JournalMedia"),
)

export const journalSchema = pipe(
	object({
		approvedSeconds: optional(pipe(number(), integer())),
		at: timestamp,
		clipCount: nonNegative,
		coveredSeconds: nonNegative,
		deflationReason: optionalString,
		id: pipe(string(), minLength(1)),
		markdown: optionalString,
		media: array(journalMediaSchema),
		mediaCount: nonNegative,
		reviewerNote: optionalString,
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

export const findingSchema = pipe(
	object({
		createdAt: timestamp,
		detail: optionalString,
		evidence: optionalString,
		externalId: pipe(string(), minLength(1)),
		id: pipe(string(), minLength(1)),
		makerAction: optionalString,
		openedBy: optionalString,
		openedSubmissionId: pipe(string(), minLength(1)),
		openedVersion: nonNegative,
		question: optionalString,
		resolution: optionalString,
		resolvedBy: optionalString,
		resolvedVersion: optional(pipe(number(), integer(), minValue(0))),
		severity: findingSeverityValues,
		station: pipe(string(), minLength(1)),
		status: findingStatusValues,
		summary: pipe(string(), minLength(1), maxLength(maxNote)),
		updatedAt: timestamp,
	}),
	title("Finding"),
)

export const findingCreateRequestSchema = pipe(
	object({
		detail: optional(pipe(string(), maxLength(maxNote))),
		evidence: optional(pipe(string(), maxLength(maxNote))),
		makerAction: optional(pipe(string(), maxLength(maxNote))),
		question: optional(pipe(string(), maxLength(200))),
		severity: findingSeverityValues,
		station: pipe(string(), minLength(1), maxLength(120), nonBlank),
		summary: pipe(string(), minLength(1), maxLength(500), nonBlank),
	}),
	title("FindingCreateRequest"),
)

export const findingUpdateRequestSchema = pipe(
	object({
		resolution: optional(pipe(string(), maxLength(maxNote))),
		status: findingStatusValues,
	}),
	title("FindingUpdateRequest"),
)

export const findingResponseSchema = pipe(object({ finding: findingSchema }), title("FindingResponse"))

export const findingsResponseSchema = pipe(object({ findings: array(findingSchema) }), title("FindingsResponse"))

export const reviewSchema = pipe(
	object({
		acknowledgements: array(string()),
		createdAt: timestamp,
		decision: pipe(string(), minLength(1)),
		deflationReason: optionalString,
		held: nonNegative,
		heldReason: optionalString,
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		id: pipe(string(), minLength(1)),
		internalNote: optionalString,
		invalidatedAt: optionalString,
		noteToMaker: string(),
		overrideReason: optionalString,
		reviewerEmail: pipe(string(), minLength(1)),
		reviewerName: optionalString,
		stage: nonNegative,
		technicalFeatures: optionalString,
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
		requireDeflationReason: pipe(number(), integer(), minValue(0), maxValue(1)),
		requireTechnicalFeatures: pipe(number(), integer(), minValue(0), maxValue(1)),
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

export const evidenceFileSchema = pipe(
	object({
		kind: pipe(string(), minLength(1)),
		path: pipe(string(), minLength(1)),
	}),
	title("EvidenceFile"),
)

// submissionFileSchema is one snapshot-listed text file fetched for in-app preview. The text is capped server-side so a board file cannot exhaust the tab.
export const submissionFileSchema = pipe(
	object({
		path: pipe(string(), minLength(1)),
		sizeBytes: nonNegative,
		text: string(),
		truncated: boolean(),
	}),
	title("SubmissionFile"),
)

export const submissionFileResponseSchema = pipe(
	object({ file: submissionFileSchema }),
	title("SubmissionFileResponse"),
)

export const evidenceCommitSchema = pipe(
	object({
		author: string(),
		date: string(),
		message: string(),
		sha: pipe(string(), minLength(1)),
	}),
	title("EvidenceCommit"),
)

export const evidenceSchema = pipe(
	object({
		commits: array(evidenceCommitSchema),
		defaultBranch: optionalString,
		error: optionalString,
		fetchedAt: timestamp,
		files: array(evidenceFileSchema),
		forge: pipe(string(), minLength(1)),
		fullName: pipe(string(), minLength(1)),
		headSha: optionalString,
		readme: optionalString,
		truncated: boolean(),
	}),
	title("Evidence"),
)

export const resubmissionEntrySchema = pipe(
	object({
		at: timestamp,
		seconds: nonNegative,
		title: optionalString,
	}),
	title("ResubmissionEntry"),
)

export const resubmissionPriceSchema = pipe(
	object({
		afterCents: nonNegative,
		beforeCents: nonNegative,
		name: pipe(string(), minLength(1)),
	}),
	title("ResubmissionPrice"),
)

export const resubmissionSchema = pipe(
	object({
		addedFiles: array(string()),
		bomAdded: array(string()),
		bomPriceChanges: array(resubmissionPriceSchema),
		bomRemoved: array(string()),
		changedEntries: array(resubmissionEntrySchema),
		commits: array(evidenceCommitSchema),
		newEntries: array(resubmissionEntrySchema),
		previousDecision: optionalString,
		previousNote: optionalString,
		previousReviewedAt: optionalString,
		previousReviewer: optionalString,
		previousVersion: nonNegative,
		removedFiles: array(string()),
	}),
	title("Resubmission"),
)

const noteSubject = pipe(string(), regex(/^(submission|project|maker)$/, "Choose a note subject."))

export const noteSchema = pipe(
	object({
		authorEmail: pipe(string(), minLength(1)),
		authorName: optionalString,
		body: pipe(string(), minLength(1)),
		createdAt: timestamp,
		id: pipe(string(), minLength(1)),
		subjectId: pipe(string(), minLength(1)),
		subjectType: noteSubject,
	}),
	title("Note"),
)

export const reviewBundleSchema = pipe(
	object({
		acknowledgements: array(string()),
		bom: array(bomLineSchema),
		checkRun: optional(checkRunSchema),
		checks: array(checkResultSchema),
		claim: optional(claimSchema),
		draft: optional(draftSchema),
		events: array(eventSchema),
		evidence: optional(evidenceSchema),
		findings: array(findingSchema),
		journals: array(journalSchema),
		notes: array(noteSchema),
		policy: reviewPolicySchema,
		priorReviews: array(priorReviewSchema),
		resubmission: optional(resubmissionSchema),
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
		deflationReason: optional(pipe(string(), maxLength(maxNote))),
		findingIds: optional(array(string())),
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		idempotencyKey: pipe(string(), minLength(1), maxLength(120)),
		internalNote: pipe(string(), maxLength(maxNote)),
		noteToMaker: pipe(string(), maxLength(maxNote)),
		overrideReason: optional(pipe(string(), maxLength(maxNote))),
		technicalFeatures: optional(pipe(string(), maxLength(maxNote))),
		version: nonNegative,
	}),
	title("DecisionRequest"),
)

export const sendBackRequestSchema = pipe(
	object({
		reason: pipe(string(), maxLength(maxNote)),
		version: nonNegative,
	}),
	title("SendBackRequest"),
)

export const sendBackResultSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
	}),
	title("SendBackResult"),
)

export const revertDecisionRequestSchema = pipe(
	object({
		reason: pipe(string(), minLength(1), maxLength(maxNote), nonBlank),
		version: nonNegative,
	}),
	title("RevertDecisionRequest"),
)

export const revertDecisionResultSchema = pipe(
	object({
		id: pipe(string(), minLength(1)),
		stage: nonNegative,
		status: pipe(string(), minLength(1)),
	}),
	title("RevertDecisionResult"),
)

export const amendDecisionRequestSchema = pipe(
	object({
		acknowledgements: array(string()),
		decision: decisionValues,
		deflationReason: optional(pipe(string(), maxLength(maxNote))),
		findingIds: optional(array(string())),
		hoursApprovedSeconds: optional(pipe(number(), integer())),
		idempotencyKey: pipe(string(), minLength(1), maxLength(120)),
		internalNote: pipe(string(), maxLength(maxNote)),
		noteToMaker: pipe(string(), maxLength(maxNote)),
		reason: pipe(string(), minLength(1), maxLength(maxNote), nonBlank),
		technicalFeatures: optional(pipe(string(), maxLength(maxNote))),
		version: nonNegative,
	}),
	title("AmendDecisionRequest"),
)

export const journalReviewRequestSchema = pipe(
	object({
		approvedSeconds: nonNegative,
		deflationReason: optional(pipe(string(), maxLength(maxNote))),
		reviewerNote: pipe(string(), maxLength(maxNote)),
		version: nonNegative,
	}),
	title("JournalReviewRequest"),
)

export const journalReviewResponseSchema = pipe(
	object({
		hoursApprovedSeconds: nonNegative,
		journal: journalSchema,
	}),
	title("JournalAdjustment"),
)

export const noteRequestSchema = pipe(
	object({
		body: pipe(string(), minLength(1), maxLength(maxNote)),
		subjectType: noteSubject,
	}),
	title("NoteRequest"),
)

export const noteCreatedSchema = pipe(object({ note: noteSchema }), title("NoteCreated"))

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
		submitted_at: optional(timestamp),
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

// A historical review sent with an already-decided ship: Ari's decision, reviewer and approved time.
export const importReviewSchema = pipe(
	object({
		acknowledgements: optional(array(string())),
		decided_at: timestamp,
		decision: decisionValues,
		hours_approved_seconds: nonNegative,
		internal_note: optionalString,
		note_to_maker: optionalString,
		reviewer_email: pipe(string(), minLength(1)),
		reviewer_name: optionalString,
		stage: pipe(number(), integer(), minValue(1), maxValue(2)),
	}),
	title("ImportReview"),
)

export const importReviewedShipSchema = pipe(
	object({
		reviews: array(importReviewSchema),
		ship: ingestRequestSchema,
		version: optional(pipe(number(), integer(), minValue(1))),
	}),
	title("ImportShip"),
)

export const importReviewedRequestSchema = pipe(
	object({ ships: array(importReviewedShipSchema) }),
	title("ImportShipsRequest"),
)

export const importReviewedResponseSchema = pipe(
	object({
		failed: array(object({ error: pipe(string(), minLength(1)), external_id: string() })),
		imported: nonNegative,
		skipped: nonNegative,
	}),
	title("ImportResult"),
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
		check_run: optional(checkRunSchema),
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
			override_reason: optionalString,
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
	findingCreateRequestSchema,
	findingUpdateRequestSchema,
	decisionRequestSchema,
	sendBackRequestSchema,
	revertDecisionRequestSchema,
	amendDecisionRequestSchema,
	journalReviewRequestSchema,
	noteRequestSchema,
	ingestRequestSchema,
	importReviewedRequestSchema,
	inviteMemberSchema,
	updateMemberSchema,
	createRoleSchema,
	updateRoleSchema,
	withdrawRequestSchema,
])

export const responseSchemas = Object.freeze([
	auditResponseSchema,
	checkHeadSchema,
	checkResultSchema,
	checkSummarySchema,
	claimResponseSchema,
	authConfigSchema,
	decisionResultSchema,
	draftResponseSchema,
	errorResponseSchema,
	eventsResponseSchema,
	fakeDeliveriesResponseSchema,
	findingResponseSchema,
	findingsResponseSchema,
	importReviewedResponseSchema,
	ingestResponseSchema,
	journalReviewResponseSchema,
	makerProfileSchema,
	meResponseSchema,
	membersResponseSchema,
	noteCreatedSchema,
	outboxResponseSchema,
	overviewResponseSchema,
	parityResponseSchema,
	queueResponseSchema,
	reviewBundleSchema,
	reviewEventSchema,
	reviewedResponseSchema,
	reviewerProfileSchema,
	reviewsResponseSchema,
	roleDefinitionSchema,
	rolesResponseSchema,
	searchResponseSchema,
	revertDecisionResultSchema,
	sendBackResultSchema,
	submissionFileResponseSchema,
	statusResponseSchema,
	withdrawResponseSchema,
])
