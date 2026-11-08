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
import {
	fakeAuthConfig,
	fakeCheckHead,
	fakeClaimResponse,
	fakeDecisionOutcome,
	fakeFakeDeliveriesResponse,
	fakeJournalAdjustment,
	fakeMakerProfile,
	fakeMembersResponse,
	fakeMeResponse,
	fakeNoteCreated,
	fakeOutboxResponse,
	fakeQueueResponse,
	fakeReviewBundle,
	fakeReviewedResponse,
	fakeReviewerProfile,
	fakeSearchResponse,
	fakeSendBackResult,
	fakeSubmissionFileResponse,
} from "../src/lib/fixtures.gen"

const realFetch = globalThis.fetch

afterEach(() => {
	globalThis.fetch = realFetch
})

interface SeenRequest {
	body: string
	method: string
	url: string
}

// stubFetch answers every endpoint from the generated fixtures and records requests.
// Overrides pin only the fields the assertions read; everything else is the schema default.
function stubFetch(seen: SeenRequest[]): void {
	const routes: { [_: string]: unknown } = {
		"DELETE /api/v1/programs/half-life/notes/n1": null,
		"GET /api/v1/auth/config": fakeAuthConfig(),
		"GET /api/v1/dev/outbox/received": fakeFakeDeliveriesResponse({ deliveries: [] }),
		"GET /api/v1/me": fakeMeResponse({ user: { email: "a@b.c" } }),
		"GET /api/v1/programs/half-life/makers/maker%40example.com": fakeMakerProfile({
			email: "maker@example.com",
		}),
		"GET /api/v1/programs/half-life/members": fakeMembersResponse({ members: [] }),
		"GET /api/v1/programs/half-life/outbox": fakeOutboxResponse({ deliveries: [] }),
		"GET /api/v1/programs/half-life/queue": fakeQueueResponse(),
		"GET /api/v1/programs/half-life/reviewed": fakeReviewedResponse(),
		"GET /api/v1/programs/half-life/reviewers/a%40b.c": fakeReviewerProfile({ email: "a@b.c" }),
		"GET /api/v1/programs/half-life/search": fakeSearchResponse(),
		"GET /api/v1/programs/half-life/submissions/sub_1": fakeReviewBundle({ submission: { id: "sub_1" } }),
		"GET /api/v1/programs/half-life/submissions/sub_1/checks/head": fakeCheckHead(),
		"GET /api/v1/programs/half-life/submissions/sub_1/file": fakeSubmissionFileResponse({
			file: { path: "board.kicad_pcb" },
		}),
		"PATCH /api/v1/programs/half-life/members/u1": {
			active: true,
			createdAt: "2026-01-01T00:00:00Z",
			email: "a@b.c",
			id: "u1",
			role: "admin",
		},
		"PATCH /api/v1/programs/half-life/submissions/sub_1/journal/e1": fakeJournalAdjustment({
			hoursApprovedSeconds: 60,
		}),
		"POST /api/v1/dev/login": fakeMeResponse({ user: { email: "a@b.c" } }),
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
		"POST /api/v1/programs/half-life/submissions/sub_1/claim": fakeClaimResponse({
			claim: { reviewerEmail: "a@b.c" },
		}),
		"POST /api/v1/programs/half-life/submissions/sub_1/decision": fakeDecisionOutcome({ id: "sub_1" }),
		"POST /api/v1/programs/half-life/submissions/sub_1/evidence/refresh": {
			commits: [],
			fetchedAt: "2026-01-01T00:00:00Z",
			files: [],
			forge: "github",
			fullName: "a/b",
			truncated: false,
		},
		"POST /api/v1/programs/half-life/submissions/sub_1/notes": fakeNoteCreated({ note: { id: "n1" } }),
		"POST /api/v1/programs/half-life/submissions/sub_1/send-back": fakeSendBackResult({ stage: 1 }),
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

		expect(await fetchAuthConfig()).toEqual(fakeAuthConfig())
		const me = await fetchMe()
		expect(me.user.email).toBe("a@b.c")
		const loginResult = await login("a@b.c")
		expect(loginResult.user.email).toBe("a@b.c")
		await logout()
		const queue = await fetchQueue()
		expect(queue.items).toHaveLength(1)
		const filteredQueue = await fetchQueue("ready", 1, 10, 25, "updated", -1)
		expect(filteredQueue.hasMore).toBe(false)
		const reviewed = await fetchReviewed()
		expect(reviewed.items).toHaveLength(1)
		expect(await searchProjects("star")).toHaveLength(1)
		const bundle = await fetchBundle("sub_1")
		expect(bundle.submission.id).toBe("sub_1")
		const evidence = await refreshRepositoryEvidence("sub_1")
		expect(evidence.fullName).toBe("a/b")
		const claim = await claimShip("sub_1")
		expect(claim.reviewerEmail).toBe("a@b.c")
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
		const decision = await decideShip("sub_1", decisionRequest)
		expect(decision.id).toBe("sub_1")
		const rerun = await rerunChecks("sub_1")
		expect(rerun.status).toBe("queued")
		expect(await fetchMembers()).toEqual([])
		const invited = await inviteMember("a@b.c", "reviewer")
		expect(invited.role).toBe("reviewer")
		const updated = await updateMember("u1", "admin", true)
		expect(updated.role).toBe("admin")
		const reviewer = await fetchReviewerProfile("a@b.c")
		expect(reviewer.email).toBe("a@b.c")
		const maker = await fetchMakerProfile("maker@example.com")
		expect(maker.email).toBe("maker@example.com")
		expect(await fetchCheckHead("sub_1")).toMatchObject({ outdated: false })
		const file = await fetchSubmissionFile("sub_1", "board.kicad_pcb")
		expect(file.path).toBe("board.kicad_pcb")
		const sentBack = await sendShipBack("sub_1", { reason: "x", version: 2 })
		expect(sentBack.stage).toBe(1)
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
