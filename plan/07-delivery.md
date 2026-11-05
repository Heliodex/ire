# 07 – Delivery

This is the build order, the checks that gate each phase, and the operational plan. Estimates assume one engineer who knows Go and SvelteKit, with a second reviewer for the parity and cutover phases. They are approximate; the exit criteria matter more than the dates.

## Phase overview

| Phase | Outcome | Rough size |
| --- | --- | --- |
| 0 | Decisions confirmed; scaffold cleaned; contract and schema started | 1 week |
| 1 | Ingest, status, outbound, idempotency, Ari-compatible edge | 2-3 weeks |
| 2 | Checks ported and parity-tested; job orchestration | 3-4 weeks |
| 3 | Stage 1 queue and review screen; decisions; claims; audit | 3 weeks |
| 4 | Stage 2, fraud provider, overrides, send-back | 2 weeks |
| 5 | Admin/settings, roster, analytics, docs, polish | 2 weeks |
| 6 | Shadow, pilot, cutover, runbook | 2-3 weeks |

The critical path is **checks → stage 1 UI → cutover**. Everything else can slip without blocking a pilot.

## Phase 0 – Foundations

### Deliverables

- Confirm the decisions in [decisions.md](./decisions.md), especially:
  - stage 2 policy defaults;
  - whether stage 1 approval requires a note;
  - fraud provider ownership and endpoint;
  - HCA client credentials and redirect URI;
  - production hostname.
- Replace the notes scaffold:
  - `program`, `user`, `membership`, `invite`, `session`, `oauth_state`;
  - `submission`, `submission_version`, `ingest_delivery`;
  - `claim`, `review`, `draft`, `audit_log`, `submission_event`;
  - `outbox`, `program_setting`, `program_counter`.
- Set up the contract modules:
  - `web/src/lib/contract.ts` exports `requestSchemas` and `responseSchemas`;
  - `contract/generate.go` runs the Valibot generator and `go-jsonschema`;
  - `wt contract --dir .` is green.
- Add the app shell:
  - auth placeholder (dev login);
  - nav;
  - empty queue/review routes;
  - error boundary;
  - design tokens.
- Add CI:
  - `go test ./...`, `go vet ./...`;
  - `wt contract --dir .` and a clean `git diff --exit-code contract/`;
  - `cd web && bun install --frozen-lockfile && bun run svelte:check && bun run lint && bun run build`;
  - `wt build --dir .` (public, no secrets).
- Decide repository hygiene. The scaffold's `.gitignore` currently ignores `web/bun.lock` and `contract/*.gen.go`. For reproducible builds, track the lockfile; for a typed contract, either track the generated Go types or generate them in CI and fail on a dirty diff. The plan recommends tracking both the lockfile and the generated contract, and generating them explicitly in the build.

### Exit criteria

- A request can create a synthetic submission, read it back, and see it in a queue list.
- The generated Go types compile against hand-written handlers.
- The static build works with no server-only SvelteKit files.
- The schema migration runs from empty and from the previous version.
- CI is green on a public checkout with no secrets.

## Phase 1 – Wire and lifecycle

### Deliverables

- Ari-compatible:
  - `POST /api/ingest/{program}` with HMAC and timestamp;
  - `POST /api/ingest/{program}/withdraw`;
  - `GET /api/ingest/{program}/status`;
  - `409 already_queued` with the existing id;
  - `401 bad_signature`, `422` field errors, `429` rate limit.
- Native:
  - `GET /api/v1/status/{program}`;
  - `GET /api/v1/reviews/{program}?since=`;
  - `GET /api/v1/events/{program}?since=`.
- Outbound:
  - outbox row in the decision transaction;
  - HMAC `timestamp.delivery_id.body`;
  - retries with backoff;
  - admin resend (new delivery id);
  - exact payload snapshot.
- Idempotency:
  - ingest delivery id unique;
  - decision idempotency key unique per submission;
  - outbox dedupe key unique.
- A **fake receiver** test that verifies the signature, parses the payload, and asserts the exact bytes Half Life's `@hackclub/ari` handler would see.
- A **fake sender** test that uses the real `@hackclub/ari` SDK to create a ship against the local app, then withdraw and status it. This is the compatibility proof.

### Exit criteria

- A synthetic ship goes ingest → queued → decision (stubbed) → outbound → fake receiver, with no manual steps.
- Replaying the same ingest delivery returns the same ship.
- Replaying the same decision idempotency key returns the same review.
- A lost outbound is recoverable via `reviews?since` without a resend.
- The `@hackclub/ari` SDK can talk to the app for create, withdraw and status.
- Half Life's webhook handler accepts a payload from the fake receiver fixture.

## Phase 2 – Checks

### Deliverables

- `internal/checks` with the interface, runner, result types and policy.
- Ported groups:
  - repo (forge client, visibility, tree, README, file kinds, BOM, design JSON);
  - commits (list, authorship, spread);
  - evidence (journal validation, Hackatime, clips, overlap);
  - demo (probe, retries, bot walls, SSRF);
  - identity (from ingest);
  - duplicate (repo/evidence/text);
- Theme policy seeded from Half Life's `THEME_REPO_REQUIREMENTS`.
- Job handlers and `/jobs/tick`.
- Check result persistence and `check_summary`.
- A fixture suite for every check.
- A parity harness that runs the new checks over a sample of real ships and prints differences.

### Exit criteria

- A repo with a PCB source, BOM and README passes; a private repo is `needs_attention`; a repo with only a `package.json` "board" fails correctly.
- A Hackatime timeout is `error`, not `0`.
- A bot-walled demo is `warn`, not `fail`.
- A re-run creates a new result; the old result is still readable.
- The parity report has no unexplained differences on the sample.
- The queue's `ready` / `needs_attention` state matches the policy table.

## Phase 3 – Stage 1 UI

### Deliverables

- Queue:
  - filters, search, pagination, counts;
  - "Start reviewing";
  - fast lane / needs a look;
  - claim state;
  - SSE updates with a "new work" chip.
- Ire bundle:
  - one endpoint, capped arrays;
  - project, maker (redacted), checks, evidence, hours, BOM, prior reviews, policy.
- Ire screen:
  - header, preflight, evidence tabs, rail, decision buttons;
  - auto-claim, heartbeat, release;
  - draft autosave;
  - decide with idempotency and stale-version check;
  - auto-advance;
  - shortcuts.
- Stage 1 policy:
  - note optional on approve by default;
  - required on changes/reject;
  - checks/checklist/fields gates;
  - hold routing (stubbed stage 2 if not built yet).
- Audit and timeline.

### Exit criteria

- A reviewer can sign in (dev or staging HCA), open a prefetched ship, approve it in two keystrokes, and land on the next ship.
- A warning requires one acknowledgement; a blocking check disables approve with a clear reason.
- A second reviewer cannot claim the same ship; a stale claim is takeable.
- A decision replay does not duplicate.
- The keyboard shortcut dialog is generated and correct.
- A mobile-width view is usable (stacked rail, sticky decision bar).

## Phase 4 – Stage 2 and fraud

### Deliverables

- Stage 2 queue and screen.
- Hold rule engine:
  - decision kinds;
  - risk;
  - hours/grant threshold;
  - reviewer probation;
  - sample;
  - always;
  - fraud.
- Held decision panel; stage history.
- Confirm / override / send-back.
- Own-decision prevention.
- Fraud provider interface, disabled implementation, fake provider.
- `check.fraud` job, stage guard, retry/error policy.
- Outbound `review.fraud`; Half Life flag integration.
- Admin override/revert/requeue.

### Exit criteria

- A held approval emits no review webhook until stage 2 confirms.
- A confirmed approval emits exactly one final event with both stages.
- An override requires a reason and records it.
- A send-back invalidates the held review and returns the ship to stage 1.
- Fraud never runs on a stage 1 path (test + code guard).
- A fraud error holds the ship; an organiser can retry or override with a reason.
- An organiser cannot confirm their own stage 1 decision.

### Current vertical slice

Implemented:

- the organiser-only second-pass queue, claim, heartbeat and release;
- confirm, reject, request-changes and send-back, with send-back invalidating the held review and returning the ship to stage 1 `ready`;
- own-decision prevention, stage 2 stale-version guard and a role guard on every second-pass action;
- a required override reason when the second pass changes the first-pass hours or decision, stored on the review and in the outbound event;
- stage 1 acknowledgements, notes, held reasons and invalidations on the review screen, plus a History tab;
- exactly one final outbound event, with both stages in the stage history.

Still to build in this phase:

- the configurable hold rule engine beyond the seeded warnings/tier/hours policy;
- the fraud provider, `check.fraud` job and `review.fraud` event;
- admin override, revert and requeue;
- reviewer probation, sampling and per-programme rule editing.

## Phase 5 – Admin, analytics, docs

### Deliverables

- Settings:
  - programme, timezone, tracks;
  - stage 1/2 policy;
  - check policy;
  - forms, snippets, default note;
  - integrations (URL, secret last-4, event selection);
  - members and invites.
- Roster and reviewer stats.
- Activity feed and audit filters.
- Reviewed list, CSV export.
- Overview stats: queue depth, time to first review, median decision time, hold rate, fraud rate, delivery health.
- `/docs`: reviewer handbook and integration contract.
- `/styleguide`.
- Empty/loading/error states across the app.

### Exit criteria

- An admin can change a stage rule and see the next review enforce it without a deploy.
- Every settings change writes an audit row with before/after.
- A reviewer can find the handbook and the shortcuts from the app.
- The integration docs match the contract tests.

## Phase 6 – Shadow, pilot, cutover

### Deliverables

- Staging deployment with HCA test client, real checks, fake fraud.
- Shadow dual-send from Half Life for a sample.
- Parity report.
- Pilot with one theme/week or reviewer group.
- Cutover runbook and rollback runbook.
- Monitoring/alerting.
- Reviewer feedback loop (a short form or Slack thread after the first week).

### Exit criteria

- A week of pilot decisions with no missed deliveries, no split-brain and no reviewer-blocking bugs.
- Queue age and decision time are within the targets.
- Rollback has been rehearsed on staging.
- The first real week of cutover has an on-call owner and a daily check.

## Testing strategy

### Unit

- Policy: every hold rule, every validation problem, every stage transition.
- Time: seconds formatting, adjustments, coverage, rounding.
- Signatures: vector from Ari's testdata; legacy and timestamped.
- Check parsers: README, BOM, file kinds, design JSON, commit authors.
- Authz: self-review, programme scope, track scope, PII redaction.
- Idempotency: ingest, decision, outbox.
- Queue cursor and filters.

### Integration

- `storage.NewTestServer` for the DB.
- `httptest` fake forge, Hackatime, Lapse, demo, fraud.
- Handler tests for every endpoint, including error envelopes.
- Job tests: old version no-op, re-run new result, retry/backoff, outbox CAS.
- Webhook round-trip: sign, send, verify, apply, dedupe.
- Claim race: two concurrent claims, one wins.

### Contract

- `wt contract --dir .` generates Go types.
- `go build ./...` proves handlers use the generated types.
- The browser client parses every response with the same schema.
- A fixture per endpoint is validated against both the Go type and the Valibot schema.
- A field rename fails CI.

### End-to-end

- Playwright against `wt dev` with fake providers:
  - sign in;
  - queue → review → approve → next;
  - warning acknowledgement;
  - request changes with note;
  - stage 2 hold → confirm;
  - override with reason;
  - fraud error → retry;
  - resubmission diff;
  - keyboard-only path.
- Run on desktop and a 390px viewport.

### Load

- Seed 10,000 submissions, 50 reviewers.
- Measure queue query, review bundle, decision transaction, outbox drain.
- Assert p95 under the 5s storage timeout with headroom; target < 200ms for queue and < 500ms for bundle at the 10k scale.
- Run the check jobs against a mocked forge with 20 concurrent jobs; ensure no DB lock storms.

### Security

- Bad signature, stale timestamp, replayed delivery.
- Private repo SSRF/link; demo redirect to 127.0.0.1/169.254.169.254.
- CSRF on browser writes.
- Self-review via direct id.
- Cross-programme id.
- PII redaction by role.
- Decision idempotency/replay.
- Claim race.
- Outbox signature and idempotency.
- Rate limit behaviour.

### Migration

- Empty → latest.
- Previous version → latest with data.
- Latest → previous code (rollback) reads the schema.
- Backup/restore on a scratch app.
- A deliberately failing migration leaves the app unready and the previous version serving.

## CI

```sh
go test ./...
go vet ./...
wt contract --dir .
git diff --exit-code contract/ web/src/lib/contract.ts
cd web && bun install --frozen-lockfile
cd web && bun run svelte:check
cd web && bun run lint
cd web && bun run build
wt build --dir . --out /tmp/review.tar.gz
```

Plus:

- a public-build job with no secrets and no `private/`;
- an e2e job with fake providers;
- a parity job (manual/nightly, not blocking every PR);
- a dependency audit (Go and Bun).

Do not claim a check passed unless it ran. A skipped check is called out in the handoff.

## Deployment and runbook

### Deploy

```sh
wt contract --dir .
go test ./...
cd web && bun run svelte:check && bun run build
wt deploy --dir . --app-host ire.<domain>
wt doctor ire
```

Before a migration:

```sh
wt backup create ire
```

After:

```sh
wt apps
wt logs ire --tail 100
wt route check ire --path /healthz
```

### Rollback

```sh
wt rollback ire <deployment-id>
```

- Code rollback only. If the migration is not backward-compatible, also restore the backup:

```sh
wt rollback ire <deployment-id> --backup <snapshot>
```

- Then verify `/healthz`, the queue, and one synthetic decision.

### Backup and restore

```sh
wt backup create ire
wt backup list ire
wt backup restore ire <snapshot>
wt backup download ire <snapshot> --out review-backup.tar.gz
```

Test a restore on staging at least once per phase. A backup that has never been restored is a hope.

### Queue stuck

1. `GET /api/v1/admin/health` – queue depth, oldest open, last tick.
2. `wt jobs ire list --status dead` – check/outbox jobs.
3. `wt logs ire --q "sub_"` – find the submission.
4. If checks are stuck: re-run the group from the review page or admin.
5. If the claim is stuck: the claim expires; `/jobs/tick` sweeps it; an admin can release.
6. If the outbox is stuck: inspect the row, fix the receiver, resend.

### Outbox failed

1. `wt logs ire --q "outbox"` and the admin outbox view.
2. Check the last HTTP status and error.
3. Verify the receiver's URL and secret.
4. Fix, then `POST /outbox/{id}/resend` (new delivery id).
5. If the receiver is down for long, it can reconcile via `reviews?since`.

### Fraud outage

- New stage 2 ships stay `fraud_pending`; stage 2 confirm is disabled.
- Retry the provider.
- If the outage is long, an admin can override with a reason; every override is audited and visible.
- Never auto-approve because the provider is down.

### Check outage

- GitHub/Hackatime/Lapse errors are `error`, not `fail`.
- The ship stays reviewable; approval may require an acknowledgement or an hours override.
- If the forge is down for a long time, an admin can waive the specific check with a reason; the waiver is per submission and audited.

### Migration failure

- The app fails readiness; the supervisor keeps the previous process/version serving.
- Read the migration error in `wt logs`.
- Do not retry blindly. Fix the statement, bump the version, test on a copy, redeploy.
- If the DB is partially changed, restore the pre-migration backup and roll back code.

### Secret rotation

- Ingest: add the new secret as the active one, keep the old for a grace window (the adapter can verify both), update Half Life, then retire the old.
- Outbound: update Ire's secret, update Half Life's verifier, then resend failed deliveries.
- HCA: rotate the client secret, redeploy, verify login.
- Store only last-4 in the DB; never log the value.

## Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Check port is larger than expected | Delays stage 1 | Start with repo + evidence + demo; add duplicates/commits later; shadow against Ari |
| Check false positives | Reviewers distrust checks | Warnings not blocks; acknowledgement; parity report; easy waiver with audit |
| Check false negatives | Fraud slips through | Fraud at stage 2; duplicate checks; sample; auditor review |
| SQLite contention | Slow decisions | Short transactions; one writer; job concurrency 4; indexes; load test |
| Large review bundles | Slow page | Caps, pagination, one bundle, prefetch; measure payload size |
| Reviewer adoption | Low usage | Ari layout and shortcuts; pilot; handbook; feedback; no forced new vocabulary |
| Stage 2 bottleneck | Queue age | Hold only what policy names; sample; organiser bypass; monitor hold rate |
| Fraud provider unavailable | Held queue | Retry; override with reason; never auto-approve |
| Outbound delivery loss | Maker not paid | Outbox + list reconciliation; no webhook-only |
| OAuth/HCA misconfiguration | No login | Staging test; exact redirect URI; dev login; documented rotation |
| Cutover split-brain | Double decision | Pilot route flag; only one surface final; Half Life `finalizeReview` refuses a resolved submission |
| Rollback loses decisions | Maker state mismatch | Reconcile via `reviews?since` before flipping; document; rehearse |
| Public repo leaks private logic | Security/privacy | Fraud provider external; no private submodule; public-build CI |
| PII exposure | Privacy | Role redaction; no PII in URLs/logs; retention job |
| Scope creep (ledger, disputes, MCP) | Slips | Non-goals; phase gates; say no in v1 |

## Open questions

Each has a recommended default so the build can proceed.

| Question | Recommendation |
| --- | --- |
| Is stage 2 a human organiser, an automated fraud gate, or both? | Both: fraud runs at stage 2; a human confirms/overrides. |
| Which decisions are held? | Approvals only by default, when a hold rule matches. Changes/rejections are final at stage 1. |
| Does stage 1 approval require a note? | No. Default note pre-filled; optional edit. Required for changes/reject. |
| Does stage 2 require a note to confirm? | No. Required on override/send-back. |
| Is fraud required for every stage 2 ship? | No. Trigger rules; default on entry to stage 2 only when policy matches. |
| Who owns the fraud provider? | A separate private service; Ire calls it with a secret. |
| Do we need a separate checks app? | No for v1. Split if check load or timeouts demand it. |
| Do we migrate closed submissions? | No blocking import. `prior_reviews` in ingest gives context. Optional report import later. |
| Do we keep Half Life's `/review` console? | Keep as a fallback during pilot; retire or link after cutover. |
| Do we keep Ari's MCP? | Defer. Reviewers have not asked; it adds a second auth surface. |
| Multi-programme UI in v1? | Data model yes; UI switcher only if a second programme exists. |
| Do reviewers see maker email? | No at stage 1; yes at stage 2/admin. |
| Do we store media? | No. Link to Half Life's R2/provider. Add reviewer attachments later with blobs. |
| Do we need disputes in v1? | No. Half Life's console owns disputes; add a read-only link/event later. |
| Do we need reviewer reauth? | Optional programme setting; off by default. HCA `prompt=login` if enabled. |
| What is the retention window? | Season + 12 months for submissions; audit longer; document and automate. |

## First two weeks

A concrete start:

**Days 1-2**

- Confirm the open questions above in a short written decision log.
- Create the production/staging hostnames and HCA OAuth clients.
- Replace the notes schema with `program`, `user`, `membership`, `session`, `submission`, `submission_version`, `ingest_delivery`, `claim`, `review`, `outbox`, `audit_log`.
- Get `wt contract` green with `IngestRequest`, `IngestResponse`, `StatusResponse`, `DecisionRequest`, `ReviewResponse`, `QueueResponse`, `ErrorResponse`.

**Days 3-5**

- Implement signed ingest and status with a fake store.
- Add `ingest_delivery` idempotency and `409 already_queued`.
- Write the fake sender test using `@hackclub/ari` and the fake receiver test using Half Life's handler fixture.
- Add the outbox table and a stub decision that writes a review + outbox.

**Days 6-8**

- Implement the queue query, counts and cursor.
- Build the queue page with skeleton/empty/error states.
- Implement claim/heartbeat/release with a unique insert and expiry.
- Add the review bundle endpoint with a stub check summary.

**Days 9-10**

- Build the review shell: header, preflight stub, evidence tabs, rail, decision buttons.
- Implement approve/changes/reject with idempotency and stale-version checks.
- Add the keyboard registry and the shortcuts dialog.
- Demo: ingest a synthetic ship, open it, approve it, see the outbound row and the next ship.

At the end of two weeks, the wire and the stage 1 loop should be real. Checks can still be stubs; the UI should not be built on a data model that the wire cannot support.

## Definition of done (v1)

- Half Life can switch to Ire by changing environment variables, and switch back.
- A reviewer can work a full queue without training beyond the handbook.
- Stage 1 approval is two keystrokes for a clean ship and never requires a note unless the reviewer wants one.
- Stage 2 holds exactly what policy says, fraud runs only there, and every hold has a visible reason.
- A lost webhook is recoverable by `GET`, not by a resend script.
- The checks pass the parity sample; every difference is documented.
- A decision, hold, override, revert and withdrawal are all audited and idempotent.
- The app deploys with `wt`, backs up, restores, rolls back and passes a load test at 10k submissions.
- The runbook exists and has been rehearsed on staging.
