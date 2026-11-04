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

### Stage 2

- [x] Organiser-only second-pass queue with the held count and a `stage=2` filter.
- [x] Organiser claim, heartbeat and release for a `stage2_hold` ship.
- [x] Reviewers get `403` on second-pass claim and decision, and the UI keeps it read-only.
- [x] Second-pass confirm, reject and request-changes actions as a final decision.
- [x] Stage 1 held review kept at stage 1; the final review is stage 2 and supersedes it.
- [x] Held hours and the stage 1 note are reused when the organiser confirms without changing them.
- [x] Own-decision prevention: the organiser who held the ship cannot confirm it.
- [x] Exactly one final outbound event, with stage 1 and stage 2 in the stage history.
- [x] Second-pass banner, hold reason and first-pass note on the review screen.
- [x] Programme-wide counts stay visible while browsing the second-pass list.
- [x] Tests for the full hold → confirm loop, role boundaries, idempotent replay and own-decision refusal.

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

- [ ] Checks cover GitHub only; GitLab and the theme group are not ported.
- [ ] Auth uses the live HCA app and an admin invite flow; roster sync and production developer-account cleanup are still missing.
- [ ] Stage 2 has the full seeded loop, but the hold rule engine and fraud provider are not built.
- [ ] Fraud is a configured minimum stage and a future provider, with no implementation.
- [ ] Evidence caches the repo README, tree and commits; journals and clips still come from the ingest payload.
- [ ] The outbound receiver defaults to in-process. HTTP delivery works, but no live Half Life staging endpoint is wired.
- [ ] The queue refreshes from storage events and keeps a 10-second poll as a fallback.
- [ ] No settings UI, analytics or secret rotation UI; the admin roster and audit browser now exist alongside the retention job.
- [x] The queue pages with `limit` (up to 100) and `offset` (up to 10000) and reports `hasMore`; the recovery feeds page with a stable `since` cursor.
- [x] A sliding-window limiter caps browser mutations at 120/min per session and machine mutations at 240/min per address; reads are unaffected.
- [ ] Duplicate, identity and demo checks exist; fraud is still a future provider.
- [ ] Roster sync is still missing; roles and active state are managed from the admin page, and identities are bound by the HCA subject.

## Next 1 – Stage 2 loop

**Goal:** a warned or held approval can be finished by an organiser instead of dead-ending in `stage2_hold`.

The loop is complete for the seeded policy: a stage 1 hold, an organiser second-pass queue, claim, confirm, override, send-back and one final event.

### Working now

- [x] Show the stage 1 acknowledgements, note and reviewer on the stage 2 screen.
- [x] Send-back invalidates the held review, returns the ship to stage 1 `ready`, clears the approved hours and records the reason and event.
- [x] Send-back is organiser-only, needs a claim and a reason, and writes no outbound event.
- [x] An override needs a reason when the second pass changes the first-pass hours or decision; the reason is stored and sent to Half Life.
- [x] A History tab shows both stages, held state, acknowledgements, overrides and invalidations.
- [x] The second-pass stale-version guard has a test.
- [x] The held review keeps its acknowledgements so the second pass can read them.
- [x] The review screen handles the second-pass empty and invalidated states.
- [x] `plan/07-delivery.md` records the implemented loop and its gaps.

### Remaining

- [ ] Add an explicit fraud stage guard test when the provider exists; there is no fraud path to guard yet.
- [ ] Extend the browser smoke script with keyboard coverage for the second pass.
- [ ] Decide whether send-back should be available to reviewers or remain organiser-only.

**Done when:** a warned ship can be held at stage 1, claimed by an organiser, confirmed, overridden or sent back at stage 2, and Half Life receives exactly one final event per final decision.

## Next 2 – Real repo checks

**Goal:** replace fixture-prescribed and payload-only results with real forge checks for new ingests, while keeping fixtures offline.

The first check pass is working. New GitHub ingests fetch the repository in the request, replace the payload approximations, and produce real results. Fixtures keep their prescribed rows. The job runner, caching and the remaining check groups are next.

### Working now

- [x] GitHub repository URL parsing; only `github.com` is fetched, so a maker-supplied host cannot be probed.
- [x] Repository metadata, default branch, private/missing detection and rate-limit errors.
- [x] README fetch, minimum-length warning and image count from Markdown and HTML sources.
- [x] BOM file detection by common names (`bom.csv`, `bom.md`, `bill-of-materials`).
- [x] Design-file detection for KiCad, Eagle, gerber, drill, STEP, STL and archives.
- [x] Commit count, distinct days and author names from the forge.
- [x] Real checks replace payload checks by key, so fixtures and new ingests share one UI.
- [x] Summary and `ready` / `needs_attention` transition; a blocking failure holds the ship.
- [x] Bounded requests with a 12-second timeout, response-size cap and optional GitHub token.
- [x] `REVIEW_GITHUB_TOKEN`, `REVIEW_GITHUB_API` and `REVIEW_DISABLE_REPO_CHECKS`.
- [x] Integration tests against a fake GitHub server, including a missing repository.
- [x] Verified against a real public repository (`C6-H/Starbie`) from the Wasm component.
- [x] Check details, groups, warning/blocking gates and the dot strip still work with real results.
- [x] Ingest marks the ship `checking` and enqueues `POST /jobs/checks` through the supervisor's durable queue.
- [x] One idempotent job per submission version, with a five-attempt retry policy.
- [x] The worker replaces repository check rows, stores the evidence snapshot and moves the ship to `ready` or `needs_attention`.
- [x] A queue failure falls back to running the checks inline; a stale `checking` ship moves to `needs_attention` after ten minutes.
- [x] The job handler skips a version that was decided, withdrawn or replaced.
- [x] `job_concurrency` is four; `REVIEW_DISABLE_JOBS=1` keeps the inline path.
- [x] Tests cover the queued path, idempotent rerun and the inline fallback.
- [x] `check_run` stores status, attempts, start/finish, last error and runner version per submission version.
- [x] The queue, review bundle and status feed expose the check run; the queue shows a Checking count and label.
- [x] The review screen shows the running attempt and reloads itself when checks finish.
- [x] A failed run records its error; after five failed attempts the ship moves to `needs_attention` immediately.
- [x] The BOM check parses CSV, TSV, semicolon and Markdown tables for part, quantity, unit price and vendor.
- [x] The BOM summary reports part count and total, warns on missing prices or vendors, and ignores subtotal/tax rows.
- [x] Commits are fetched since the previous approval for a resubmission, or the programme start for a first version.
- [x] A full-history fallback keeps repositories older than the programme record visible.
- [x] Hackatime links are read from the public stats endpoint; a failed refresh is `error` with cached values, never zero.
- [x] Hackatime checks cover linked projects, tracked time and staleness, with a five-minute cache.
- [x] Journal checks cover presence, clip-only entries and journal time against the claim.
- [x] Clip checks cover count, coverage and HTTPS link validity.
- [x] Organisers can rerun repository checks from the review rail; the run is queued with a unique idempotency key and the page polls it.
- [x] The checks rail shows a failed attempt and its error.
- [x] Check results carry a `source` (`fixture`, `forge`, `payload`, `probe`) exposed through the bundle.
- [x] A public HTTPS demo is probed once per check job; loopback, private and link-local targets are rejected.
- [x] Duplicate repository/demo links and Hackatime/Slack identities reused across maker emails warn on ingest.

### Remaining

- [x] The review rail compares the snapshot head with the live GitHub head and offers a rerun when the repository moved.
- [x] Forge responses are cached per request with ETag revalidation, a short commit window and a 30-second negative cache; rerun and evidence refresh clear it.
- [x] The BOM parse is compared with the maker's submitted lines; a price mismatch over 5% warns (`repo.bom.prices`).
- [ ] Add GitLab behind the same forge interface.
- [ ] Port the remaining group: theme requirements.
- [ ] Add Hackatime span overlap against timelapses and the reviewer override path.
- [ ] Scope Hackatime totals to the phase week instead of the all-time project total.
- [x] Tests cover rate limits, a dropped forge connection, malformed JSON, an oversized tree, BOM parsing and matching, commit windows and the check job runner.
- [ ] Add a fixture parity suite and a sample run over real `.projects.csv` rows (not committed).
- [x] `plan/04-checks.md` lists every emitted key with its group, source, severity, blocking rule and Ari porting map.

**Done when:** checks run as a job, a real PCB repo with a BOM, README and design files reaches `ready`, a private or missing repo reaches `needs_attention`, a timeout is `error` rather than `0`, and the queue state matches the check policy.

## Next 3 – Hack Club Auth and staging webhook

**Goal:** real reviewer identity and a real signed delivery to a staging Half Life receiver, ready for a pilot.

The auth flow, real OAuth app and signed HTTP delivery are implemented and tested. Pointing at the staging receiver, the cutover rehearsal and the remaining account safeguards are left.

### Working now

- [x] `GET /api/v1/auth/config` publishes which methods are enabled.
- [x] `packages/v2/oauth` handles OIDC discovery, PKCE, token exchange, RS256 ID-token verification and userinfo.
- [x] OAuth state, verifier and nonce live in a short-lived `login_flow` row and are consumed once.
- [x] `packages/v2/session` owns the session and OAuth-state cookies, hashed tokens and same-origin checks.
- [x] The app builds against the v2 SDK module.
- [x] Inbound signatures are verified through `packages/v2/sign`.
- [x] Deliveries use `packages/v2/outbox` with leases, stored attempt history and its `webhook_outbox` table; old rows are migrated once.
- [x] `GET /auth/login` creates a short-lived state cookie and redirects to Hack Club Auth.
- [x] `GET /auth/callback` verifies state, exchanges the code and fetches `/api/v1/me`.
- [x] Identity is mapped to an existing active membership by email; unknown accounts are refused, never auto-created.
- [x] Roles come from the membership, not from OAuth claims or client input.
- [x] Session creation is shared by HCA and development login; logout is `/api/v1/logout`.
- [x] Hack Club Auth scopes are `openid profile email`; the provider's legal name is not used.
- [x] Browser mutations reject a cross-site `Origin` through `session.SameOrigin`.
- [x] The redirect URI falls back to the public origin when `HC_REDIRECT_URI` is unset.
- [x] Secrets resolve through `secret://` references in `app.json`; the live app now reports `hca: true`.
- [x] One configured `REVIEW_BOOTSTRAP_EMAIL` can create its first membership; every other unknown account is refused.
- [x] `server_pages` sends root and protected page loads through the component; signed-in root and `/login` go to `/queue`, while signed-out protected pages go to `/` before any page renders.
- [x] Review URLs use `/review/{id}`; the old query form redirects, and static assets stay absolute so hydration works below the root.
- [x] Development accounts and login are gated by `REVIEW_DEV_AUTH`; seeding dev accounts is skipped when it is off.
- [x] The login screen shows `Continue with Hack Club` when HCA is configured and keeps dev accounts for local use.
- [x] Stage 2 actions are limited to organiser/admin roles.
- [x] Tests cover the auth config, a full fake-provider flow, bad state and an unknown member.
- [x] Signed HTTP outbound delivery is tested against a local receiver that verifies `timestamp.delivery_id.body`.
- [x] `REVIEW_OUTBOUND_URL` can point at a real HTTPS receiver; retries and delivery rows already exist.

### Remaining

- [x] Register the real Hack Club Auth application and set `HC_CLIENT_ID`, `HC_CLIENT_SECRET` and `HC_REDIRECT_URI` through `wt secrets`.
- [ ] Point `REVIEW_OUTBOUND_URL` at the staging Half Life receiver and set its signing secret.
- [x] Admins invite emails and change roles or active state from `/admin`, with an audit trail.
- [ ] Roster sync from Half Life or Hack Club is still missing; invited members must be added by email.
- [x] Store the stable Hack Club subject in `user_identity` so an email change cannot break the binding.
- [x] Refuse a second Hack Club subject for an account and show `identity_conflict` on the login screen.
- [x] Signing in rotates an existing browser session; admins can revoke every session for a member.
- [x] Tests cover inactive membership, an expired login flow and session replacement.
- [ ] Store the Hack Club `slack_id` claim again; the shared OAuth identity does not expose it.
- [ ] Test provider timeout and duplicate identity binding end to end.
- [x] Outbound tests cover a 5xx retry, a dropped connection retry and permanent failure on 4xx.
- [ ] Send one approved, one changes and one rejected event to staging; verify Half Life applies each.
- [x] `REVIEW_OUTBOUND_DRY_RUN=1` records deliveries as sent without calling the receiver, so a pilot can compare before cutover.
- [x] `plan/08-runbook.md` covers the shadow window, cutover, recovery feeds, rollback and incident checks.
- [x] `plan/08-runbook.md` documents `REVIEW_DEV_AUTH=0` and the SQL that removes the seeded accounts from a production copy.
- [x] The runbook includes the delete for the three seeded emails before the pilot.

**Done when:** a real HCA reviewer can sign in and complete a clean approval, and the staging Half Life receiver verifies and applies the signed final event.

## Next 4 – Evidence snapshot

**Goal:** fetch and cache the evidence reviewers currently open in other tabs, without slowing the first paint.

The repository half is working: ingest caches the README, file tree and commits, the Evidence tab shows them, and an organiser can refresh the cache. Journal and clip evidence is validated at ingest; Markdown rendering and richer tests remain.

### Working now

- [x] `repo_snapshot` stores forge, full name, default branch, head SHA, README, files, commits, fetch time and error per submission version.
- [x] The snapshot is written by the check job after the repository fetch; the inline fallback writes it in the ingest path.
- [x] README is capped at 200 KB; the tree at 2,000 blob paths; commits at 200 with a truncated flag.
- [x] File paths are labelled by kind (`bom`, `readme`, `design`, `doc`, `image`, `code`, `file`) without storing contents.
- [x] Commit rows keep sha, author, date and the first line of the message.
- [x] Evidence rides on the review bundle as an optional field, so fixtures and old rows stay valid.
- [x] An Evidence tab lists the files and commits, shows the README as preformatted text and never renders repository HTML.
- [x] Organisers can refresh the snapshot with `POST /evidence/refresh`; reviewers get `403`.
- [x] A refresh replaces the row for the current version and leaves checks and decisions untouched.
- [x] Empty, error and narrow-screen states are handled; the tab strip wraps instead of cutting off.
- [x] Fake-forge tests cover snapshot persistence, the refresh role gate, one row per version and bundle delivery.
- [x] Verified against a real public repository (`C6-H/Starbie`) from the deployed component.

### Remaining

- [ ] Store co-authors and merge commits, and mark commits made after submission.
- [x] Store journal images and clip URLs from the ingest payload with counts and coverage.
- [ ] Validate the remaining evidence and media URLs as HTTPS; do not proxy project binaries.
- [x] Forge requests share the ETag cache, the 30-second unavailable cache and the review rate limits.
- [x] Render README Markdown through parsed blocks; raw HTML, images and link targets never reach the page.
- [x] The evidence tab filters files by path and depth, and searches commits by message, author or SHA, with clear empty states.
- [x] The review rail shows a stale notice and a rerun action when the head SHA has moved since the snapshot.
- [x] Parsed Markdown never keeps raw HTML or image syntax; an oversized tree is capped and marked truncated. Oversized README, paginated commit and refresh failure tests remain.
- [ ] Document what is stored, what is linked, retention and privacy.

**Done when:** opening a ship shows its README, file tree, commits, journals and clips without leaving Review, the data is cached for the version, and a new version refreshes it.

## Cross-cutting work for the four steps

- [x] The queue has offset pagination; review and event feeds have stable `since` cursors.
- [x] Subscribe the queue to the supervisor storage event stream, keeping the 10-second poll as a fallback.
- [x] Enforce same-origin checks on state-changing browser routes.
- [x] Request bodies are bounded on ingest, withdraw and JSON routes; browser and machine mutations are rate limited.
- [x] `/admin` lists recent audit entries; a review History tab already shows both stages, holds, overrides and invalidations.
- [x] `/admin` manages the roster, roles (including organizers), active state and session revocation.
- [x] `GET /metrics` exposes queue depth, active claims, stage 1 hold rate, time to first review, check duration and delivery counts in Prometheus text.
- [x] The scheduled tick prunes login flows, ingest deliveries, closed-ship snapshots, audit records and fake receipts on fixed windows.
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
