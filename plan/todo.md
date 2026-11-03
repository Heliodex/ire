# MVP todo

This is the working checklist for the Half Life review service. It records what the vertical slice already does, what is deliberately stubbed, and the four next steps: stage 2, real repo checks, Hack Club Auth plus a staging webhook, and the evidence snapshot.

The plan in [`plan/`](./) remains the design source. This file is the build order and the acceptance list.

## How to run it

```sh
wt dev --dir . --port 8080
```

Open the `dev: vite at …` URL that the command prints. Sign in with a seeded account (`reviewer@example.com`, `organiser@example.com`, `admin@example.com`) or call `POST /api/v1/dev/reset` to reseed the three fixtures.

```sh
go test ./...
go vet ./...
golangci-lint run ./...
cd web && bun run svelte:check && bun run build
wt contract --dir .
wt build --dir .
```

## Done so far

### Plan and foundations

- [x] Product, architecture, data model, checks, UI, integration and delivery written in `plan/`.
- [x] Decision log covering Ari compatibility, ownership, stages and fraud boundary.
- [x] `wt` scaffold, generated contract pipeline and static SvelteKit build.
- [x] SQLite schema for programmes, people, memberships, sessions, ships, versions, deliveries, evidence, checks, claims, reviews, drafts, timeline, audit, outbox and settings.
- [x] Seed programme, development accounts, settings and fixtures; idempotent seed that repairs partial fixture rows.
- [x] Development reset endpoint for rehearsals.
- [x] `.projects.csv` kept out of git; fixture data reduced and PII-free.

### Ingest and Ari edge

- [x] `POST /api/ingest/{program}` and `POST /api/v1/ingest/{program}`.
- [x] HMAC over the raw body, and over `timestamp.body` when `X-Ari-Timestamp` is present.
- [x] Five-minute timestamp window.
- [x] Legacy body-only signature behind `allow_legacy_signature`, failing closed when the setting is missing.
- [x] `X-Ari-Delivery-Id` replay protection and body-hash fallback.
- [x] `201 accepted`, `200 duplicate`, and `409 already_queued` with the open ship id.
- [x] New version after withdraw or a closed ship; versions stored with payload and SHA-256.
- [x] Withdraw endpoint, idempotent for closed ships.
- [x] Field validation for required ids, URLs and hour limits.
- [x] Ari-compatible `GET /api/ingest/{program}/status` by `external_id` or `id`, including explicit `decision: null` for open ships.
- [x] Status phase mapping (`processing`, `review`, `second_pass`, `reviewed`, `withdrawn`), stage history, checks, timeline and approved review object.
- [x] `GET /api/v1/reviews/{program}?since=…` returning the exact outbound payload.
- [x] `GET /api/v1/events/{program}?since=…` for queue transitions.
- [x] Ingest records the maker, description, repo/demo/thumbnail URLs, theme, phase, week, tier, grant cap, hours, BOM and prior reviews.

### Auth and sessions

- [x] `GET /api/v1/me` with user and programme memberships.
- [x] Development login for seeded accounts, hashed session tokens, `HttpOnly` cookies and expiry.
- [x] Development logout and `REVIEW_DEV_AUTH=0` switch.
- [x] Reviewer, organiser, admin and auditor roles in the schema and memberships.
- [x] Programme scoping on queue, bundle, claim, draft, decision and outbox routes.
- [x] Maker email redaction in the review bundle for reviewers.
- [x] Maker email redaction in outbox debug payloads for reviewers.

### Queue

- [x] Stage 1 queue with `all`, `fast`, `attention` and `mine` filters.
- [x] Ready, in review, needs attention and second-pass counts.
- [x] Claim, heartbeat, release and claim expiry.
- [x] `409` when another reviewer holds the claim; own claim can be refreshed.
- [x] Queue response includes the latest compact check result per key for the dots.
- [x] Queue response includes `thumbnailUrl`.
- [x] Row thumbnails from local fixture assets, with a fallback icon.
- [x] Coloured check dots per result: pass, warn, changes, reject, skip.
- [x] J/K replaced by `↑`/`↓` with scroll-into-view; `Enter` opens.
- [x] `←`/`→` switches filters and keeps focus on the selected filter.
- [x] Per-filter cache with stale-while-revalidate, no skeleton flash after first load.
- [x] Client-side search.
- [x] Responsive layouts for wide, medium and thin widths.
- [x] Wide order: thumbnail, project, maker, status, type, version, hours, age, checks.
- [x] Medium order: title, badges/checks, maker/version plus right-aligned timing.
- [x] Thin order: title, status/type plus checks, maker/version plus stacked timing.
- [x] Status and project type aligned in all layouts.
- [x] Version aligned in fixed columns.
- [x] Check-indicator alignment in the thin layout.
- [x] Request changes toned orange; reject red; approve green.

### Review screen

- [x] Single bundle endpoint with submission, checks, journals, BOM, reviews, events, prior reviews, claim, draft, acknowledgements and policy.
- [x] Overview with edge-to-edge cover image, description, claimed/journal/Hackatime/BOM stats and timeline.
- [x] Checks tab grouped as repo/evidence with status, label, key, description and acknowledgement.
- [x] Full-width check description below the columns.
- [x] Check key beside the label, moving to its own line below 500px.
- [x] Journal tab with hours, dates, media counts, clip counts and coverage.
- [x] BOM tab with part links, quantities, vendors and unit totals.
- [x] `Unit (US$)` and `Total (US$)` headers with bare amounts.
- [x] Warning acknowledgement gate; blocking checks disable approve.
- [x] Decision rail with inline approved hours, claimed total, maker note and internal note.
- [x] Draft autosave and server-side draft restore.
- [x] Stale-version and missing-claim guards.
- [x] Decision idempotency key per ship/version, preserved across retries.
- [x] Auto-advance to the next ready ship.
- [x] `A` approve, `C` focus note, `R` reject, `Esc` leave, `?` shortcuts.
- [x] Claim release on navigation and page leave; heartbeat pauses in background tabs.
- [x] Responsive rail and mobile layout.

### Decisions, holds and outbound

- [x] Stage 1 decisions: approved, changes, rejected.
- [x] Note required for changes/rejection when policy says so.
- [x] Approved hours above the claim are held.
- [x] Warning approvals are held when `holdOnWarn` is on.
- [x] Held tiers and minimum approved hours follow the seeded policy.
- [x] Changes and rejections are always final and delivered.
- [x] Stage 1 route refuses a ship already on the second pass.
- [x] Stage 2 hold writes no outbox row.
- [x] Outbox written in the same transaction as the review.
- [x] Signed outbound HMAC over `timestamp.delivery_id.body` with `X-Ari-*` headers.
- [x] Retry ladder from 30 seconds to 24 hours; 4xx failures permanent.
- [x] In-process fake receiver verifies the signature and timestamp window.
- [x] Delivery page showing outbox rows and verified receiver bodies.
- [x] Audit log and submission timeline entries for decisions, claims, releases, holds and deliveries.
- [x] Consolidated outbound event with stage history, maker, ship, review, checks and approved hours.

### Checks and evidence in the slice

- [x] Payload-only local checks: repo URL, description length, images, BOM, journal detail, claimed hours.
- [x] Fixture ships carry prescribed repo/README/BOM/design-file/commit/evidence results.
- [x] Three fixtures from real `.projects.csv` rows: clean, warned and blocked.
- [x] Local project images for all three fixtures.
- [x] Warning/blocking semantics shared between queue, review screen and decision gate.

### Quality

- [x] Go handler tests for signing, replay, conflict, resubmission, queue, claims, drafts, decisions, holds, idempotency, redaction, Hackatime projects, legacy fail-closed behaviour and tick.
- [x] Contract generated from `web/src/lib/contract.ts`.
- [x] `svelte-check`, static build, `go test`, `go vet`, `golangci-lint` and `wt build` passing.
- [x] Route and browser smoke checks for queue, review, deliveries and theme toggle.
- [x] Commits kept atomic through the UI and data work.

## Deliberately fake or incomplete

- [ ] Checks are payload-only. No GitHub/GitLab fetch, tree, README, commit or Hackatime call.
- [ ] Auth is the development account table, not Hack Club Auth.
- [ ] Stage 2 is a status and a hold policy, not a working second-pass queue or decision.
- [ ] Fraud is a configured minimum stage and a future provider, with no implementation.
- [ ] Evidence is whatever Half Life sends in the payload. Review does not fetch or snapshot the repo.
- [ ] The outbound receiver is in-process. No live Half Life staging endpoint is wired.
- [ ] No SSE. The queue polls every 10 seconds.
- [ ] No admin or settings UI, roster, analytics, audit browser, retention jobs or secret rotation UI.
- [ ] No pagination beyond the current limits; queue limit is 50, feeds are 100.
- [ ] No rate limiting or CSRF token beyond the session cookie and same-site policy.
- [ ] No duplicate, identity, demo, Hackatime, Lapse or fraud checks.
- [ ] No `check_run` table; results are written directly with no run status or retry record.
- [ ] The `checking` status is not shown in the queue; ingest runs local checks synchronously.
- [ ] No live HCA identities, programme roster sync or role administration.

## Next 1 – Stage 2 loop

**Goal:** a warned or held approval can be finished by an organiser instead of dead-ending in `stage2_hold`.

### Contract and data

- [ ] Decide the stage 2 API shape: reuse `POST …/decision` with the ship stage inferred, or add `POST …/stage2/decision` and a stage field. Prefer an explicit stage 2 route so the role gate is obvious.
- [ ] Add stage 2 queue response items and counts to the contract, or reuse `queue?stage=2` with a stage 2 filter.
- [ ] Extend the bundle with the stage 1 held review, hold reason, stage history and stage 2 permissions.
- [ ] Keep `review` rows for both stages: stage 1 held (`is_final = 0`) and stage 2 final (`is_final = 1`).
- [ ] Add `supersedes_review_id` linking the stage 2 review to the held stage 1 review.
- [ ] Ensure the `review_final_idx` unique index allows exactly one final review.
- [ ] Add a stage 2 capability to the `me` response or derive it from role and programme membership.

### Backend

- [ ] Allow an organiser/admin to claim a `stage2_hold` ship, with a stage 2 claim and TTL.
- [ ] Reject stage 2 claims by reviewers and stage 1 claims by reviewers on held ships.
- [ ] Add a `stage2` claim/release/heartbeat path that keeps the same expiry and conflict rules.
- [ ] Allow stage 2 approve, reject and changes as a final decision.
- [ ] Do not require the organiser to re-acknowledge stage 1 warnings; show the stage 1 acknowledgements instead.
- [ ] Copy or reference the stage 1 note and approved hours on the stage 2 screen.
- [ ] Allow the organiser to adjust approved hours at stage 2.
- [ ] Add own-decision prevention: the organiser who made the stage 1 decision cannot confirm it at stage 2.
- [ ] Add optional send-back: invalidate the held review, return the ship to stage 1 `ready`, record the reason and event.
- [ ] Update the submission to `decided`, `stage = 2`, final hours and `decided_at`.
- [ ] Write exactly one final outbound event with both stage history entries and no hold.
- [ ] Deduplicate the final event by the stage 2 review id.
- [ ] Record audit entries for stage 2 claim, decision, override and send-back.
- [ ] Gate fraud at stage 2 only in the code path, even while the provider is missing.

### UI

- [ ] Add a `Second pass` entry to the header or queue filter with the held count.
- [ ] Build a stage 2 list or reuse the queue with a second-pass filter and organizer-only visibility.
- [ ] Show a stage 2 banner with the hold reason, stage 1 reviewer, stage 1 note, approved checks and hours.
- [ ] Add stage 2 actions: confirm, override with reason, reject and send back.
- [ ] Keep reviewers out of stage 2 actions; show a read-only view or hide the entry.
- [ ] Support the same claim/heartbeat/auto-advance and keyboard shortcuts.
- [ ] Add a stage history panel that shows both stages.
- [ ] Add responsive checks and empty states for the second-pass queue.

### Tests and docs

- [ ] Stage 1 warning approval → hold → organiser claim → stage 2 approve → one delivered final event.
- [ ] Stage 2 status and stage history reflect both stages.
- [ ] Reviewer receives `403` on stage 2 claim and decision.
- [ ] Organiser cannot confirm their own stage 1 decision.
- [ ] Send-back returns the ship to stage 1 and invalidates the held review.
- [ ] Stage 2 idempotency and stale-version tests.
- [ ] Fraud stage guard test.
- [ ] Update README demo, seed accounts and the plan delivery notes.

**Done when:** a warned ship can be held at stage 1, claimed by an organiser, confirmed or sent back at stage 2, and Half Life receives exactly one final event per final decision.

## Next 2 – Real repo checks

**Goal:** replace fixture-prescribed and payload-only results with real forge checks for new ingests, while keeping fixtures offline.

### Data and contract

- [ ] Add `check_run` with submission, version, status, attempts, started/finished, error and runner version.
- [ ] Add a `source` column or field to check results (`fixture`, `forge`, `payload`, `manual`).
- [ ] Add `checking` to queue responses, counts and the status contract, or keep it hidden with a clear refresh path.
- [ ] Add a `checks running` state to the review bundle and queue.
- [ ] Add `data` JSON to the API for check details (file path, table row, commit range).
- [ ] Define the check key, label, group and severity catalogue in one place shared by the runner and UI.
- [ ] Add configuration for a GitHub token and, later, GitLab tokens.

### Checks to port, in order

- [ ] `repo.accessible`: parse the forge URL, fetch repo metadata, detect private, missing and rate-limited.
- [ ] `repo.readme`: presence, length, description, build steps and image count.
- [ ] `repo.readme.images`: Markdown and HTML image sources, deduplicated, ignoring tiny tracking images.
- [ ] `repo.bom`: find BOM/CSV/table files, parse headers and rows, require a minimum row count.
- [ ] `repo.design_files`: KiCad, Eagle, gerber, drill, schematic and board file kinds.
- [ ] `repo.commits`: commit count, first/last date, distinct days, authors and co-authors in the window.
- [ ] `repo.license` or `repo.tree` if the policy needs it.
- [ ] `evidence.journals`: keep the existing payload validation.
- [ ] `evidence.hours`: keep the claimed/journal breakdown and add Hackatime when available.
- [ ] `evidence.clips`: coverage from ingested clip seconds, warning only.
- [ ] `demo.probe`: optional hardware demo probe with SSRF guards, retries and bot-wall handling.
- [ ] `identity`: show the ingest verification state; do not collect documents.
- [ ] `duplicate`: same repo, evidence or description across open/approved ships, warning only.
- [ ] Theme policy from Half Life's `THEME_REPO_REQUIREMENTS`: required files per theme.

### Forge client and runner

- [ ] Define a forge interface for metadata, tree, README, commits and file contents.
- [ ] Implement GitHub first; add GitLab behind the same interface later.
- [ ] Use a token from `wt secrets`; keep requests bounded and time-limited.
- [ ] Cache by forge, repo and head SHA, with ETag where available.
- [ ] Restrict fetches to known forge hosts; never fetch an arbitrary maker-supplied URL.
- [ ] Cap tree size, README size, file count and commit pages.
- [ ] Run checks in a bounded batch from `/jobs/tick`, not in the ingest request.
- [ ] Ingest writes `checking`, enqueues or records a run, and returns immediately.
- [ ] Runner marks each check pass/warn/fail/error/skip with data and duration.
- [ ] Compute the summary, transition to `ready` or `needs_attention`, and record events.
- [ ] Record retries and terminal errors; never turn a network error into a pass.
- [ ] Make runs idempotent per submission/version and keep old results readable.
- [ ] Skip real network checks for fixture ships; mark their source as `fixture`.
- [ ] Re-run checks for a new version and keep the old version's results.

### UI

- [ ] Show a checking state with a short poll while checks run.
- [ ] Show real check details, file paths and table lines.
- [ ] Show a check error distinctly from a fail.
- [ ] Show a rerun/refresh affordance for organisers.
- [ ] Keep the warning/blocking gates and the dot strip driven by real results.
- [ ] Add a "checks outdated" notice when a version changes.

### Tests and docs

- [ ] Unit tests for BOM table parsing, README images, file kind detection and commit window maths.
- [ ] Integration tests against a fake forge over `httptest`.
- [ ] Private repo, 404, rate limit, timeout, malformed JSON and oversized tree cases.
- [ ] Queue transition tests: checking → ready/needs_attention.
- [ ] Retry/idempotency tests for the runner.
- [ ] Fixture parity suite and a sample run over real `.projects.csv` rows (not committed).
- [ ] Document check keys, severities, blocking rules, token setup and the porting map.

**Done when:** a real PCB repo with a BOM, README and design files reaches `ready`, a private or missing repo reaches `needs_attention`, a timeout is `error` rather than `0`, and the queue state matches the check policy.

## Next 3 – Hack Club Auth and staging webhook

**Goal:** real reviewer identity and a real signed delivery to a staging Half Life receiver, ready for a pilot.

### Hack Club Auth

- [ ] Register the OAuth application; record client id, secret and redirect URLs per environment.
- [ ] Add the auth secrets through `wt secrets`, never the repository.
- [ ] Add `/auth/login` with state and PKCE; set a short-lived state cookie.
- [ ] Add `/auth/callback`; verify state, exchange the code and fetch the identity.
- [ ] Map the HCA identity to `user` and `membership`; do not auto-create arbitrary programme members.
- [ ] Add an allowlist or roster mapping for the Half Life programme.
- [ ] Assign reviewer, organiser, admin and auditor roles from membership, not from client input.
- [ ] Reject unknown, inactive or unverified members with a clear message.
- [ ] Create, rotate and expire sessions; set `Secure`, `HttpOnly` and `SameSite`.
- [ ] Disable development login in production; keep it explicitly enabled only for local dev.
- [ ] Add CSRF protection for state-changing browser routes.
- [ ] Enforce the stage 2 organiser/admin role.
- [ ] Tests for bad state, expired code, unknown user, inactive membership, role mapping and session fixation.
- [ ] Remove the seeded accounts from any production path.

### Staging webhook

- [ ] Stand up or choose the staging Half Life receiver and its signing secret.
- [ ] Point `REVIEW_OUTBOUND_URL` at the staging receiver; verify the exact body and HMAC.
- [ ] Confirm the receiver enforces the timestamp window, delivery id and replay rules.
- [ ] Send one approved, one changes and one rejected event; verify Half Life applies each.
- [ ] Verify a held stage 2 approval emits nothing until the organiser confirms.
- [ ] Test retry on 5xx/timeout and permanent failure on 4xx.
- [ ] Expose delivery status and the last error in the UI; link to the outbox payload.
- [ ] Add a shadow mode or a dry-run receiver for the first pilot days.
- [ ] Document the cutover, rollback and replay path (`reviews?since`, `events?since`).
- [ ] Rotate the ingest and outbound secrets and document the procedure.

**Done when:** a real HCA reviewer can sign in and complete a clean approval, and the staging Half Life receiver verifies and applies the signed final event.

## Next 4 – Evidence snapshot

**Goal:** fetch and cache the evidence reviewers currently open in other tabs, without slowing the first paint.

### Data and fetcher

- [ ] Add `repo_snapshot` with submission, version, forge, repo, head SHA, tree, README, commits, fetched_at, error and ETag.
- [ ] Add a job or tick step that fetches the README, tree and commits after the repo checks.
- [ ] Use the commit window from the programme start or previous approval through the submission time.
- [ ] Store commit count, authors, co-authors, first/last date and distinct days.
- [ ] Store file paths and kinds, not file contents, unless a specific check asks for them.
- [ ] Cap README, tree, commit pages and total payload size.
- [ ] Store media and clip URLs and metadata; do not proxy or copy project binaries.
- [ ] Validate evidence URLs as HTTPS; do not fetch private or non-allowlisted hosts.
- [ ] Make snapshot fetches idempotent per submission/version and refreshable.
- [ ] Record fetch errors without turning them into a check failure.
- [ ] Reuse the forge cache and rate-limit handling from the checks work.

### UI

- [ ] Add an Evidence tab or dock for README, file tree, commits, journals and clips.
- [ ] Render README Markdown sanitised; treat HTML as text, never raw.
- [ ] Add a file tree browser with paths and file-kind icons.
- [ ] Add a commit list with author, date and message; mark after-submit commits.
- [ ] Show journal images and clips from the ingest payload with counts and coverage.
- [ ] Show the last fetched time and a refresh action for organisers.
- [ ] Keep the review screen fast: lazy-load evidence, cap lists, no giant images.
- [ ] Support mobile stacking and the existing tab/keyboard patterns.

### Tests and docs

- [ ] Fake-forge tests for README, tree, commits and snapshot persistence.
- [ ] Markdown/HTML sanitisation tests, including script and iframe payloads.
- [ ] Oversized README/tree and paginated commit tests.
- [ ] Snapshot idempotency and refresh tests.
- [ ] Snapshot error and stale-state UI tests.
- [ ] Document what is stored, what is linked, retention and privacy.

**Done when:** opening a ship shows its README, file tree and commits without leaving Review, the data is cached for the version, and a new version refreshes it.

## Cross-cutting work for the four steps

- [ ] Add pagination and stable cursors to queue, reviews and events.
- [ ] Add SSE or a lightweight change feed instead of the 10-second queue poll.
- [ ] Add request rate limits and body limits to browser and machine routes.
- [ ] Add a proper CSRF strategy for browser mutations.
- [ ] Add audit-browser and review-history UI.
- [ ] Add admin roster and role management, including stage 2 organisers.
- [ ] Add metrics for queue depth, time to first review, hold rate, check duration and delivery failures.
- [ ] Add retention for sessions, deliveries, snapshots and audit.
- [ ] Add secret rotation and environment documentation.
- [ ] Keep every step behind tests and a deployable `wt build`.

## Later

- [ ] Stage 2 fraud provider and `review.fraud` outbound event.
- [ ] Admin override, revert and requeue.
- [ ] Analytics for review throughput and check outcomes.
- [ ] Ari compatibility parity report over real ships.
- [ ] Pilot shadow mode, cutover and rollback rehearsal.
- [ ] Documentation site for reviewers and integrators.

## Suggested order

1. Stage 2 loop, because held ships currently dead-end.
2. Real repo checks for new ingests, starting with accessible, README and BOM.
3. Hack Club Auth and the staging webhook, before any real reviewer touches it.
4. Evidence snapshot, to remove the remaining tab-switching from stage 1.

Steps 2 and 3 can overlap once the stage 2 loop is stable: the checks work is backend-heavy, the auth work is infrastructure-heavy.
