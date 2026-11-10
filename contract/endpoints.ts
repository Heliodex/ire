import {
	authConfigSchema,
	checkHeadSchema,
	checkRunSchema,
	claimResponseSchema,
	decisionResultSchema,
	evidenceSchema,
	fakeDeliveriesResponseSchema,
	findingResponseSchema,
	findingsResponseSchema,
	journalReviewResponseSchema,
	makerProfileSchema,
	memberSchema,
	membersResponseSchema,
	meResponseSchema,
	noteCreatedSchema,
	outboxResponseSchema,
	overviewResponseSchema,
	queueResponseSchema,
	reviewBundleSchema,
	reviewedResponseSchema,
	reviewerProfileSchema,
	revertDecisionResultSchema,
	roleDefinitionSchema,
	rolesResponseSchema,
	searchResponseSchema,
	sendBackResultSchema,
	stationAnswerResponseSchema,
	stationQueueResponseSchema,
	stationsResponseSchema,
	submissionFileResponseSchema,
} from "../web/src/lib/contract"

export interface EndpointParam {
	default?: string
	in: "path" | "query" | "body" | "init"
	map?: { [_: string]: string }
	name: string
	optional?: boolean
	type: string
	value?: string
	when?: string
	wire?: string
	wrap?: boolean
}

export interface Endpoint {
	doc?: string
	keepalive?: boolean | string
	method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE"
	name: string
	params?: EndpointParam[]
	path: string
	response: "void" | { schema: unknown; select?: string[] }
}

// The endpoint registry is the second half of the contract: schemas describe the shapes, entries describe how to call them. `wt contract` turns each entry into a typed fetch wrapper in web/src/lib/api.gen.ts. Keep the doc lines – they become the code comments.
//
// saveDraft and fetchAudit stay hand-written in web/src/lib/api.ts: the draft fills per-field defaults and the audit keeps its filters object, neither of which the registry expresses.
export const endpoints: Endpoint[] = Object.freeze([
	{
		doc: "fetchAuthConfig returns which sign-in methods are enabled.",
		method: "GET",
		name: "fetchAuthConfig",
		path: "/api/v1/auth/config",
		response: { schema: authConfigSchema },
	},
	{
		doc: "fetchMe returns the signed-in reviewer, throwing ApiError 401 when there is no session.",
		method: "GET",
		name: "fetchMe",
		path: "/api/v1/me",
		response: { schema: meResponseSchema },
	},
	{
		doc: "login is the development sign-in placeholder for Hack Club Auth.",
		method: "POST",
		name: "login",
		params: [{ in: "body", name: "email", type: "string", wrap: true }],
		path: "/api/v1/dev/login",
		response: { schema: meResponseSchema },
	},
	{
		doc: "logout deletes the session cookie.",
		method: "POST",
		name: "logout",
		path: "/api/v1/logout",
		response: "void",
	},
	{
		doc: "fetchQueue returns one page of the stage 1 work list. A sort key orders every page server-side so appended rows keep arriving at the end.",
		method: "GET",
		name: "fetchQueue",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ default: '"all"', in: "query", name: "filter", type: "string" },
			{ default: "1", in: "query", name: "stage", type: "number" },
			{ default: "0", in: "query", name: "offset", type: "number" },
			{ default: "50", in: "query", name: "limit", type: "number" },
			{
				default: "null",
				in: "query",
				name: "sort",
				optional: true,
				type: "string | null",
			},
			{
				default: "1",
				in: "query",
				map: { "-1": "desc", "1": "asc" },
				name: "dir",
				type: "1 | -1",
				when: "sort",
			},
			{
				default: '""',
				in: "query",
				name: "q",
				type: "string",
			},
		],
		path: "/api/v1/programs/{program}/queue",
		response: { schema: queueResponseSchema },
	},
	{
		doc: "fetchStations lists the workflow stations and how many open ships are waiting in each.",
		method: "GET",
		name: "fetchStations",
		params: [{ in: "path", name: "program", type: "string", value: "program" }],
		path: "/api/v1/programs/{program}/stations",
		response: { schema: stationsResponseSchema },
	},
	{
		doc: "fetchStationQueue lists the open ships whose current station is this one.",
		method: "GET",
		name: "fetchStationQueue",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "station", type: "string" },
		],
		path: "/api/v1/programs/{program}/stations/{station}/queue",
		response: { schema: stationQueueResponseSchema },
	},
	{
		doc: "saveStationAnswer records one station question's verdict and evidence.",
		method: "PUT",
		name: "saveStationAnswer",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{ in: "path", name: "station", type: "string" },
			{ in: "path", name: "question", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof stationAnswerRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/stations/{station}/answers/{question}",
		response: { schema: stationAnswerResponseSchema },
	},
	{
		doc: "fetchOverview returns the programme health readout: wait percentiles, today counts, status breakdown, leaderboard and daily activity.",
		method: "GET",
		name: "fetchOverview",
		params: [{ in: "path", name: "program", type: "string", value: "program" }],
		path: "/api/v1/programs/{program}/overview",
		response: { schema: overviewResponseSchema },
	},
	{
		doc: "fetchReviewed returns a page of decided ships, newest first.",
		method: "GET",
		name: "fetchReviewed",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ default: '"all"', in: "query", name: "decision", type: "string" },
			{ default: "0", in: "query", name: "offset", type: "number" },
			{ default: "50", in: "query", name: "limit", type: "number" },
		],
		path: "/api/v1/programs/{program}/reviewed",
		response: { schema: reviewedResponseSchema },
	},
	{
		doc: "searchProjects finds open ships by title, maker, external id or theme for the header search.",
		method: "GET",
		name: "searchProjects",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "query", name: "query", type: "string", wire: "q" },
			{ default: "8", in: "query", name: "limit", type: "number" },
		],
		path: "/api/v1/programs/{program}/search",
		response: { schema: searchResponseSchema, select: ["results"] },
	},
	{
		doc: "fetchBundle returns everything the review screen shows.",
		method: "GET",
		name: "fetchBundle",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}",
		response: { schema: reviewBundleSchema },
	},
	{
		doc: "refreshRepositoryEvidence refetches the cached repository snapshot for an organiser.",
		method: "POST",
		name: "refreshRepositoryEvidence",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/evidence/refresh",
		response: { schema: evidenceSchema },
	},
	{
		doc: "claimShip takes or refreshes the claim.",
		method: "POST",
		name: "claimShip",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/claim",
		response: { schema: claimResponseSchema, select: ["claim"] },
	},
	{
		doc: "heartbeat extends the claim while the tab is open.",
		method: "POST",
		name: "heartbeat",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/heartbeat",
		response: "void",
	},
	{
		doc: "releaseShip gives the ship back to the queue. keepalive lets the request finish when the review page unmounts.",
		keepalive: true,
		method: "DELETE",
		name: "releaseShip",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/claim",
		response: "void",
	},
	{
		doc: "reviewJournal records one entry's approved time and deflation note.",
		keepalive: "keepalive",
		method: "PATCH",
		name: "reviewJournal",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{ in: "path", name: "entry", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof journalReviewRequestSchema>",
			},
			{ default: "false", in: "init", name: "keepalive", type: "boolean" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/journal/{entry}",
		response: { schema: journalReviewResponseSchema },
	},
	{
		doc: "fetchFindings lists every finding that follows this project across versions.",
		method: "GET",
		name: "fetchFindings",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/findings",
		response: { schema: findingsResponseSchema },
	},
	{
		doc: "createFinding records a problem or note that survives a resubmission.",
		method: "POST",
		name: "createFinding",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof findingCreateRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/findings",
		response: { schema: findingResponseSchema },
	},
	{
		doc: "updateFinding resolves, accepts, disputes or withdraws a finding.",
		method: "PATCH",
		name: "updateFinding",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{ in: "path", name: "finding", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof findingUpdateRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/findings/{finding}",
		response: { schema: findingResponseSchema },
	},
	{
		doc: "createNote adds an internal note on the ship, its project or its maker.",
		method: "POST",
		name: "createNote",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof noteRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/notes",
		response: { schema: noteCreatedSchema, select: ["note"] },
	},
	{
		doc: "deleteNote removes an internal note.",
		method: "DELETE",
		name: "deleteNote",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/notes/{id}",
		response: "void",
	},
	{
		doc: "decideShip records the stage 1 decision.",
		method: "POST",
		name: "decideShip",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof decisionRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/decision",
		response: { schema: decisionResultSchema },
	},
	{
		doc: "rerunChecks queues a fresh repository check for an organiser.",
		method: "POST",
		name: "rerunChecks",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/checks/rerun",
		response: { schema: checkRunSchema },
	},
	{
		doc: "fetchMembers lists the programme roster.",
		method: "GET",
		name: "fetchMembers",
		params: [{ in: "path", name: "program", type: "string", value: "program" }],
		path: "/api/v1/programs/{program}/members",
		response: { schema: membersResponseSchema, select: ["members"] },
	},
	{
		doc: "inviteMember adds an email to the roster.",
		method: "POST",
		name: "inviteMember",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "body", name: "email", type: "string" },
			{ in: "body", name: "role", type: "string" },
		],
		path: "/api/v1/programs/{program}/members",
		response: { schema: memberSchema },
	},
	{
		doc: "updateMember changes a role or reactivates an account.",
		method: "PATCH",
		name: "updateMember",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{ in: "body", name: "role", type: "string" },
			{ in: "body", name: "active", type: "boolean" },
		],
		path: "/api/v1/programs/{program}/members/{id}",
		response: { schema: memberSchema },
	},
	{
		doc: "fetchRoles lists the programme role definitions and their permissions.",
		method: "GET",
		name: "fetchRoles",
		params: [{ in: "path", name: "program", type: "string", value: "program" }],
		path: "/api/v1/programs/{program}/roles",
		response: { schema: rolesResponseSchema, select: ["roles"] },
	},
	{
		doc: "createRole adds a programme role with an explicit permission set.",
		method: "POST",
		name: "createRole",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof createRoleSchema>",
			},
		],
		path: "/api/v1/programs/{program}/roles",
		response: { schema: roleDefinitionSchema },
	},
	{
		doc: "updateRole replaces a role's permission set. Locked roles always allow everything.",
		method: "PATCH",
		name: "updateRole",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "name", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof updateRoleSchema>",
			},
		],
		path: "/api/v1/programs/{program}/roles/{name}",
		response: { schema: roleDefinitionSchema },
	},
	{
		doc: "deleteRole removes a role that no roster row uses.",
		method: "DELETE",
		name: "deleteRole",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "name", type: "string" },
		],
		path: "/api/v1/programs/{program}/roles/{name}",
		response: "void",
	},
	{
		doc: "fetchReviewerProfile returns one reviewer's roster entry, stats and recent decisions.",
		method: "GET",
		name: "fetchReviewerProfile",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "key", type: "string" },
		],
		path: "/api/v1/programs/{program}/reviewers/{key}",
		response: { schema: reviewerProfileSchema },
	},
	{
		doc: "fetchMakerProfile returns one maker's identity and their latest ship per project.",
		method: "GET",
		name: "fetchMakerProfile",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "email", type: "string" },
		],
		path: "/api/v1/programs/{program}/makers/{email}",
		response: { schema: makerProfileSchema },
	},
	{
		doc: "fetchCheckHead compares the snapshot with the live repository head.",
		method: "GET",
		name: "fetchCheckHead",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/checks/head",
		response: { schema: checkHeadSchema },
	},
	{
		doc: "fetchSubmissionFile returns the capped text of one snapshot-listed KiCad file for in-app preview.",
		method: "GET",
		name: "fetchSubmissionFile",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{ in: "query", name: "path", type: "string" },
		],
		path: "/api/v1/programs/{program}/submissions/{id}/file",
		response: { schema: submissionFileResponseSchema, select: ["file"] },
	},
	{
		doc: "sendShipBack invalidates a held review and returns the ship to stage 1.",
		method: "POST",
		name: "sendShipBack",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof sendBackRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/send-back",
		response: { schema: sendBackResultSchema },
	},
	{
		doc: "revertDecision invalidates the final review and reopens the ship. Allowed for admins or the reviewer who made that decision.",
		method: "POST",
		name: "revertDecision",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof revertDecisionRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/decision/revert",
		response: { schema: revertDecisionResultSchema },
	},
	{
		doc: "amendDecision replaces the final review with a corrected decision. Allowed for admins or the reviewer who made that decision.",
		method: "POST",
		name: "amendDecision",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "path", name: "id", type: "string" },
			{
				in: "body",
				name: "request",
				type: "InferOutput<typeof amendDecisionRequestSchema>",
			},
		],
		path: "/api/v1/programs/{program}/submissions/{id}/decision/amend",
		response: { schema: decisionResultSchema },
	},
	{
		doc: "fetchOutbox returns recent deliveries for the operations page.",
		method: "GET",
		name: "fetchOutbox",
		params: [
			{ in: "path", name: "program", type: "string", value: "program" },
			{ in: "query", name: "limit", type: "number", value: "'50'" },
		],
		path: "/api/v1/programs/{program}/outbox",
		response: { schema: outboxResponseSchema, select: ["deliveries"] },
	},
	{
		doc: "fetchReceived returns what the in-process receiver verified.",
		method: "GET",
		name: "fetchReceived",
		params: [{ in: "query", name: "limit", type: "number", value: "'50'" }],
		path: "/api/v1/dev/outbox/received",
		response: { schema: fakeDeliveriesResponseSchema, select: ["deliveries"] },
	},
])
