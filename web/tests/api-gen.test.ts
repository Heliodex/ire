import { afterEach, describe, expect, test } from "bun:test"
import {
	claimShip,
	createNote,
	decideShip,
	deleteNote,
	fetchAuthConfig,
	fetchBundle,
	fetchCheckHead,
	fetchMakerProfile,
	fetchMe,
	fetchMembers,
	fetchOutbox,
	fetchQueue,
	fetchReceived,
	fetchReviewed,
	fetchReviewerProfile,
	fetchSubmissionFile,
	heartbeat,
	inviteMember,
	login,
	logout,
	refreshRepositoryEvidence,
	releaseShip,
	rerunChecks,
	reviewJournal,
	searchProjects,
	sendShipBack,
	updateMember,
} from "../src/lib/api"
import { ApiError } from "../src/lib/api-error"

const realFetch = globalThis.fetch

afterEach(() => {
	globalThis.fetch = realFetch
})

interface SeenRequest {
	body: string
	method: string
	url: string
}

const submission = {
	externalId: "fix-clean:design",
	hoursClaimedSeconds: 3600,
	hoursHackatimeSeconds: 0,
	hoursJournalSeconds: 3600,
	id: "sub_1",
	ingestedAt: "2026-01-01T00:00:00Z",
	makerEmail: "maker@example.com",
	stage: 1,
	status: "ready",
	title: "Board",
	track: "hardware",
	updatedAt: "2026-01-02T00:00:00Z",
	version: 1,
}

const policy = {
	fraudMinStage: 2,
	holdMinApprovedSeconds: 0,
	holdOnWarn: 1,
	holdTiers: [],
	noteRequiredForChanges: 1,
}

const bundle = {
	acknowledgements: [],
	bom: [],
	checks: [],
	events: [],
	journals: [],
	notes: [],
	policy,
	priorReviews: [],
	reviews: [],
	submission,
}

// stubFetch answers every endpoint with a valid payload and records requests.
function stubFetch(seen: SeenRequest[]): void {
	const routes: { [_: string]: unknown } = {
		"DELETE /api/v1/programs/half-life/notes/n1": null,
		"GET /api/v1/auth/config": { dev: true, hca: false },
		"GET /api/v1/dev/outbox/received": { deliveries: [] },
		"GET /api/v1/me": { programs: [], readOnly: false, user: { email: "a@b.c", id: "u1" } },
		"GET /api/v1/programs/half-life/makers/maker%40example.com": {
			email: "maker@example.com",
			submissions: [],
		},
		"GET /api/v1/programs/half-life/members": { members: [] },
		"GET /api/v1/programs/half-life/outbox": { deliveries: [] },
		"GET /api/v1/programs/half-life/queue": {
			counts: { checking: 0, claimed: 0, fast: 0, mine: 0, needsAttention: 0, ready: 0, stage2Hold: 0 },
			hasMore: false,
			items: [],
		},
		"GET /api/v1/programs/half-life/reviewed": { hasMore: false, items: [] },
		"GET /api/v1/programs/half-life/reviewers/a%40b.c": {
			email: "a@b.c",
			permissions: [],
			reviews: [],
			reviewTotal: 0,
			stats: {
				approved: 0,
				changes: 0,
				firstPass: 0,
				held: 0,
				hoursApprovedSeconds: 0,
				invalidated: 0,
				rejected: 0,
				secondPass: 0,
				total: 0,
			},
			tracks: [],
		},
		"GET /api/v1/programs/half-life/search": { results: [] },
		"GET /api/v1/programs/half-life/submissions/sub_1": bundle,
		"GET /api/v1/programs/half-life/submissions/sub_1/checks/head": { outdated: false },
		"GET /api/v1/programs/half-life/submissions/sub_1/file": {
			file: { path: "board.kicad_pcb", sizeBytes: 10, text: "board", truncated: false },
		},
		"PATCH /api/v1/programs/half-life/members/u1": {
			active: true,
			createdAt: "2026-01-01T00:00:00Z",
			email: "a@b.c",
			id: "u1",
			role: "admin",
		},
		"PATCH /api/v1/programs/half-life/submissions/sub_1/journal/e1": {
			hoursApprovedSeconds: 60,
			journal: {
				at: "2026-01-01T00:00:00Z",
				clipCount: 0,
				coveredSeconds: 0,
				id: "e1",
				mediaCount: 0,
				seconds: 60,
			},
		},
		"POST /api/v1/dev/login": { programs: [], readOnly: false, user: { email: "a@b.c", id: "u1" } },
		"POST /api/v1/programs/half-life/members": {
			active: true,
			createdAt: "2026-01-01T00:00:00Z",
			email: "a@b.c",
			id: "u1",
			role: "reviewer",
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/checks/rerun": {
			attempts: 1,
			runnerVersion: "v1",
			startedAt: "2026-01-01T00:00:00Z",
			status: "queued",
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/claim": {
			claim: { expiresAt: "2026-01-02T00:00:00Z", isMine: 1, reviewerEmail: "a@b.c" },
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/decision": {
			held: 0,
			id: "sub_1",
			status: "decided",
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/evidence/refresh": {
			commits: [],
			fetchedAt: "2026-01-01T00:00:00Z",
			files: [],
			forge: "github",
			fullName: "a/b",
			truncated: false,
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/notes": {
			note: {
				authorEmail: "a@b.c",
				body: "x",
				createdAt: "2026-01-01T00:00:00Z",
				id: "n1",
				subjectId: "sub_1",
				subjectType: "submission",
			},
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/send-back": { id: "sub_1", stage: 1, status: "ready" },
	}
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub implements only the call signature the client uses, not fetch's statics.
	globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
		const url = input instanceof Request ? input.url : input.toString()
		const method = init?.method ?? "GET"
		seen.push({ body: typeof init?.body === "string" ? init.body : "", method, url })
		const body = routes[`${method} ${new URL(url, "http://localhost").pathname}`] ?? null
		return Promise.resolve(Response.json(body, { status: 200 }))
	}) as unknown as typeof fetch
}

const decisionRequest = {
	acknowledgements: [] as string[],
	decision: "approved" as const,
	hoursApprovedSeconds: 3600,
	idempotencyKey: "k",
	internalNote: "",
	noteToMaker: "",
	version: 1,
}

describe("generated client", () => {
	test("every endpoint returns its contract shape", async () => {
		const seen: SeenRequest[] = []
		stubFetch(seen)

		expect(await fetchAuthConfig()).toEqual({ dev: true, hca: false })
		expect((await fetchMe()).user.email).toBe("a@b.c")
		expect((await login("a@b.c")).user.email).toBe("a@b.c")
		await logout()
		expect((await fetchQueue()).items).toEqual([])
		expect((await fetchQueue("ready", 1, 10, 25, "updated", -1)).hasMore).toBe(false)
		expect((await fetchReviewed()).items).toEqual([])
		expect(await searchProjects("star")).toEqual([])
		expect((await fetchBundle("sub_1")).submission.id).toBe("sub_1")
		expect((await refreshRepositoryEvidence("sub_1")).fullName).toBe("a/b")
		expect((await claimShip("sub_1")).reviewerEmail).toBe("a@b.c")
		await heartbeat("sub_1")
		await releaseShip("sub_1")
		expect(await reviewJournal("sub_1", "e1", { approvedSeconds: 60, reviewerNote: "", version: 1 })).toMatchObject(
			{ hoursApprovedSeconds: 60 },
		)
		expect(
			await reviewJournal("sub_1", "e1", { approvedSeconds: 60, reviewerNote: "", version: 1 }, true),
		).toMatchObject({ hoursApprovedSeconds: 60 })
		expect(await createNote("sub_1", { body: "x", subjectType: "submission" })).toMatchObject({ id: "n1" })
		await deleteNote("n1")
		expect((await decideShip("sub_1", decisionRequest)).id).toBe("sub_1")
		expect((await rerunChecks("sub_1")).status).toBe("queued")
		expect(await fetchMembers()).toEqual([])
		expect((await inviteMember("a@b.c", "reviewer")).role).toBe("reviewer")
		expect((await updateMember("u1", "admin", true)).role).toBe("admin")
		expect((await fetchReviewerProfile("a@b.c")).email).toBe("a@b.c")
		expect((await fetchMakerProfile("maker@example.com")).email).toBe("maker@example.com")
		expect(await fetchCheckHead("sub_1")).toEqual({ outdated: false })
		expect((await fetchSubmissionFile("sub_1", "board.kicad_pcb")).path).toBe("board.kicad_pcb")
		expect((await sendShipBack("sub_1", { reason: "x", version: 2 })).stage).toBe(1)
		expect(await fetchOutbox()).toEqual([])
		expect(await fetchReceived()).toEqual([])
	})

	test("request shapes match the backend encoding", async () => {
		const seen: SeenRequest[] = []
		stubFetch(seen)

		await fetchQueue("ready", 1, 10, 25, "updated", -1)
		await fetchQueue()
		await fetchSubmissionFile("sub_1", "board file.kicad_pcb")
		await login("a@b.c")
		await inviteMember("a@b.c", "reviewer")
		await decideShip("sub_1", decisionRequest)
		await heartbeat("sub_1")
		await searchProjects("star")

		const queue = seen.filter(request => request.url.includes("/queue"))
		expect(queue[0]?.url).toContain("sort=updated")
		expect(queue[0]?.url).toContain("dir=desc")
		expect(queue[1]?.url).not.toContain("sort=")
		expect(seen.some(request => request.url.includes("/submissions/sub_1/heartbeat"))).toBe(true)
		expect(seen.find(request => request.url.includes("/search"))?.url).toContain("q=star")

		const bodies = Object.fromEntries(
			seen
				.filter(request => request.body !== "")
				.map(request => [new URL(request.url, "http://localhost").pathname, request.body]),
		)
		expect(JSON.parse(bodies["/api/v1/dev/login"])).toEqual({ email: "a@b.c" })
		expect(JSON.parse(bodies["/api/v1/programs/half-life/members"])).toEqual({
			email: "a@b.c",
			role: "reviewer",
		})
		expect(JSON.parse(bodies["/api/v1/programs/half-life/submissions/sub_1/decision"])).toEqual(decisionRequest)
	})

	test("errors map the same way", async () => {
		// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- The stub implements only the call signature the client uses, not fetch's statics.
		globalThis.fetch = (() =>
			Promise.resolve(
				Response.json({ code: "gone", error: "No ship" }, { status: 404 }),
			)) as unknown as typeof fetch

		const failure = await fetchBundle("missing").then(
			() => null,
			(err: unknown) => err,
		)

		expect(failure).toBeInstanceOf(ApiError)
	})
})
