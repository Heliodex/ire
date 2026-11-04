# Half Life Review

A review service for Half Life hardware ships. This is the working vertical slice of the plan in [`plan/`](./plan): a real signed ingest, a real SQLite queue, stage 1 review, a working second pass, queued GitHub repository checks and a signed outbox. Reviewer auth uses Hack Club Auth over OpenID Connect when configured, with development accounts as a fallback, and GitHub evidence is cached per ship.

## What works

- Ari-compatible ingest at `POST /api/ingest/{program}` with HMAC over the raw body, timestamp support, replay protection and `409 already_queued`.
- Ari-compatible status at `GET /api/ingest/{program}/status`, withdraw, and the native `reviews?since` / `events?since` recovery feeds.
- A stage 1 queue with claims, heartbeats, drafts, warning acknowledgements, blocking checks, decisions and idempotency keys.
- A stage 2 hold policy for approvals: a warning, a held tier, or hours above the claim waits for the second pass and emits no webhook. Changes and rejections are final and always deliver. The stage 1 endpoints refuse a ship that is already on the second pass.
- A second-pass queue for organisers, with claim, confirm, reject, changes, send-back and override-with-reason actions, stage history and one final outbound event. Reviewers can read a held ship but cannot decide it.
- Real GitHub checks for new ingests, queued as a durable supervisor job so ingest returns immediately: repository access, README and image count, BOM file, design files and commit history. Fixtures keep their prescribed results.
- Hack Club Auth sign-in for roster members over OpenID Connect discovery, PKCE, nonce and a verified RS256 ID token. Development sign-in remains for local use.
- A cached repository snapshot per ship: README, capped file tree, commit list and head SHA, shown in the Evidence tab. Organisers can refresh it without re-running the checks.
- Signed HTTP outbound delivery to a real receiver; the in-process receiver is only the default development target.
- A signed outbox written in the decision transaction, an in-process receiver that verifies `timestamp.delivery_id.body`, retries on a schedule and keeps the exact payload.
- The queue refreshes from the supervisor storage event stream, with polling as a fallback.
- Three fixture ships extracted from real `.projects.csv` rows: clean, warned and blocked.

## Look

The interface follows Ari's visual language: Hanken Grotesk, Ari's grey surfaces, the red primary, pill badges and compact controls. The header toggle switches light and dark; the choice is stored in `localStorage` and otherwise follows the system preference.

## Run it

```sh
wt dev --dir . --port 8080
```

Open the `dev: vite at …` URL that it prints (the frontend is served by Vite in development; the API is on `review.localhost:8080`). For a production build, run `wt build --dir .` and `wt deploy --dir .`; the supervisor then serves both the static site and the API on `review.localhost`. The root and protected pages are selected server-side with `server_pages` and `app.StaticPage`: a signed-in root redirects to `/queue`, a signed-out root serves the login page, and `/queue`, `/review` and `/deliveries` redirect to `/` without loading the page first. A ship opens at `/review/{id}`; old `/review?id=…` links redirect to that path. The queue pages with `limit` and `offset` and shows a load-more control; the machine feeds page with `since`. Organisers can rerun the repository checks from the review rail; the run is queued with the same fallback as ingest and the page follows it until it finishes. The rail compares the snapshot head with the live GitHub head and shows a stale notice with that action when the repository moved. `/login` remains as a compatibility redirect to `/`.

Sign in with one of the seeded accounts:

| Email | Role |
| --- | --- |
| `reviewer@example.com` | Reviewer |
| `organiser@example.com` | Organiser |
| `admin@example.com` | Admin |

The first request runs the schema and seeds the three fixtures. `.projects.csv` is ignored by git; the fixture content is a reduced, PII-free copy with synthetic maker addresses.

### The accepted demo

1. **Clean ship** – open `Starbie`, press `A`. The decision is approved, the outbox delivers immediately, and the queue advances.
2. **Open `Deliveries`** – the outbox row is `delivered`; the receiver shows `verified`, the delivery id, timestamp and the exact body.
3. **Warned ship** – open `OpenTrace`; approve is locked until each warning is acknowledged. After acknowledgement the approval is held for the second pass and writes no outbox row.
4. **Second pass** – sign in as `organiser@example.com`, choose `Second pass`, open `OpenTrace` and press `Confirm approval` (or `A`). The final event is delivered exactly once and the ship leaves the queue.
5. **Blocked ship** – open `Smart Alarm Clock`; approve is disabled by the repository check. Request changes or reject with a note.
6. **Recovery** – `GET /api/v1/reviews/half-life?since=...` returns the same payload the webhook delivered; `GET /api/ingest/half-life/status?external_id=fix-clean:design` shows `reviewed` with the approved hours.

Reset the demo at any time with `POST /api/v1/dev/reset` while signed in. It deletes the ships and reseeds the three fixtures without touching the programme or the reviewer accounts.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `REVIEW_INGEST_SECRET` | `dev-ingest-secret` | Verifies Ari inbound signatures and the status bearer |
| `REVIEW_OUTBOUND_SECRET` | `dev-outbound-secret` | Signs outbound webhooks |
| `REVIEW_OUTBOUND_URL` | `fake://receiver` | `fake://receiver` keeps deliveries in-process; an `https://` URL posts them |
| `REVIEW_SEED_FIXTURES` | `1` | `0` starts with an empty queue |
| `REVIEW_DEV_AUTH` | `1` | `0` disables the development sign-in endpoint |
| `REVIEW_GITHUB_TOKEN` | empty | GitHub token for the repository checks; raises the API rate limit |
| `REVIEW_GITHUB_API` | `https://api.github.com` | GitHub API base, useful for a fake server in tests |
| `HACKATIME_API_BASE` | `https://hackatime.hackclub.com` | Hackatime API base for the linked-project time check |
| `REVIEW_DISABLE_REPO_CHECKS` | `0` | `1` skips network checks and keeps the payload-only checks |
| `REVIEW_DISABLE_JOBS` | `0` | `1` runs repository checks inline instead of enqueuing a job |
| `HC_CLIENT_ID` | empty | Hack Club Auth OAuth client id |
| `HC_CLIENT_SECRET` | empty | Hack Club Auth OAuth client secret |
| `HC_REDIRECT_URI` | empty | Registered callback; when empty it is derived from the public origin |
| `HC_AUTH_BASE` | `https://auth.hackclub.com` | Auth provider base, useful for a fake provider in tests |
| `REVIEW_BOOTSTRAP_EMAIL` | empty | One HCA email allowed to create its first membership; set it as a secret and disable it after the roster is seeded |
| `REVIEW_BOOTSTRAP_ROLE` | `admin` | Role given to the bootstrap email: admin, organizer, reviewer or auditor |

Set real secrets with `wt secrets set review REVIEW_INGEST_SECRET`, `REVIEW_OUTBOUND_SECRET`, `REVIEW_GITHUB_TOKEN`, `HC_CLIENT_ID`, `HC_CLIENT_SECRET`, `HC_REDIRECT_URI` and `REVIEW_BOOTSTRAP_EMAIL` before deploying. **The development login is not Hack Club Auth.** It exists for local use and must be disabled with `REVIEW_DEV_AUTH=0` before a real programme uses it.

The HCA application must allow the `openid`, `profile` and `email` scopes and the exact callback URI. Hack Club Auth uses `openid profile email`, OIDC discovery, PKCE, nonce and RS256 ID-token verification against the provider JWKS, plus the existing programme roster. The stable HCA subject is stored in `user_identity`, so changing the email on a Hack Club account does not break the link; a second subject for the same account is refused. `REVIEW_BOOTSTRAP_EMAIL` can admit exactly one configured HCA email so the first administrator can sign in; `*` admits any email only while `REVIEW_DEV_AUTH` is on, for local testing, and never applies in production. Disable it after the roster is seeded by setting the secret to an address that cannot sign in, or by removing the env reference and redeploying. An admin can invite an email and choose reviewer, organizer, admin or auditor at `/admin`, which also lists recent audit actions; a verified account without an active membership is refused, except for the one configured bootstrap email; every other account stays refused unless a roster row exists. OAuth state, verifier and nonce live in a short-lived `login_flow` row, and browser mutations require a same-origin request. Request bodies are bounded, browser mutations are limited to 120 per minute per session, and signed machine mutations to 240 per minute per address. The scheduled tick prunes expired sessions, login flows, old ingest deliveries, closed-ship snapshots, audit records and fake receipts.

### Repository checks and evidence

New ingests write the payload checks, mark the ship `checking` and enqueue `POST /jobs/checks` with an idempotency key per submission version. The queue shows the checking count and each ship's attempt, and the review screen reloads itself when the job finishes. `check_run` stores the run status, attempts, timestamps, last error and runner version, and both the review bundle and the status feed expose it. The supervisor worker calls the component through its loopback port and retries failures; if enqueueing fails, ingest runs the checks inline so the ship never waits in `checking`. Five failed attempts move the ship to `needs_attention` immediately, and a stale `checking` ship is caught by the tick after ten minutes. Each GitHub request has a 12-second timeout. A private or missing repo is a blocking failure; a short README, a missing or unparseable BOM, missing design files or no commits are warnings; a GitHub outage is an error that holds the ship for attention. The BOM check parses CSV, TSV, semicolon and Markdown tables, reports the part count and total, and warns when prices or vendors are missing. Commits are counted since the programme start, falling back to the full history for repositories older than the programme record. Linked Hackatime projects are read from the public stats endpoint with a five-minute cache; a failed refresh is an error with cached values, never a silent zero. Totals are all-time until the phase window is configured, and the check says so rather than presenting them as the claimed week. Journal checks cover entry presence, clip-only entries, journal time against the claim, clip count, coverage and HTTPS clip links. A public HTTPS demo is probed once per check job, with loopback and private-address targets rejected; bot-walled demos are left for a human. Reused repository or demo links and a Hackatime or Slack identity used across maker emails warn on ingest. The repository BOM is compared with the maker's submitted lines, and unit prices more than 5% apart warn as `repo.bom.prices`. Check results carry a `source` so the API and scheduler can distinguish fixture, payload, forge and probe results. Only `github.com` repositories are fetched in this slice; other forges keep the payload checks and show a warning. Fixtures skip the network and keep their prescribed rows.

The check job stores a snapshot in `repo_snapshot`: the README (capped at 200 KB), the first 2,000 blob paths with a kind, the first 200 commits with author and date, and the head SHA. The Evidence tab shows it as text and lists, never as rendered HTML, so a repository cannot inject markup into Review. A refresh replaces the row for the current version and does not change checks or decisions.

## API

| Method | Path | Auth |
| --- | --- | --- |
| `POST` | `/api/ingest/{program}` | HMAC `X-Ari-Signature`, optional timestamp and delivery id |
| `POST` | `/api/ingest/{program}/withdraw` | HMAC |
| `GET` | `/api/ingest/{program}/status?external_id=...` | Bearer ingest secret |
| `GET` | `/api/v1/reviews/{program}?since=...` | Bearer ingest secret |
| `GET` | `/api/v1/events/{program}?since=...` | Bearer ingest secret |
| `GET` | `/api/v1/auth/config` | Public method discovery |
| `GET` | `/metrics` | Prometheus counters (machine bearer token) |
| `GET` | `/auth/login` | Hack Club Auth start |
| `GET` | `/auth/callback` | Hack Club Auth callback |
| `POST` | `/api/v1/logout` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/evidence/refresh` | Organiser or admin |
| `POST` | `/api/v1/dev/login` | Development only |
| `GET` | `/api/v1/programs/{program}/queue?stage=1\|2` | Session; stage 2 needs organiser/admin |
| `GET` | `/api/v1/programs/{program}/submissions/{id}` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/claim` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/heartbeat` | Session |
| `DELETE` | `/api/v1/programs/{program}/submissions/{id}/claim` | Session |
| `PUT` | `/api/v1/programs/{program}/submissions/{id}/draft` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/decision` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/send-back` | Organiser or admin |
| `GET` | `/api/v1/programs/{program}/outbox` | Session |
| `POST` | `/jobs/tick` | Supervisor schedule |
| `POST` | `/jobs/checks` | Supervisor durable job |

An ingest body follows Ari's documented shape, with `meta.bom`, `meta.hours` and `meta.prior_reviews` added. Sign the exact bytes:

```sh
BODY='{"external_id":"demo:design", ...}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$REVIEW_INGEST_SECRET" -hex | awk '{print $2}')
curl -X POST http://review.localhost:8080/api/ingest/half-life \
  -H "X-Ari-Signature: $SIG" -H 'Content-Type: application/json' --data "$BODY"
```

The current `@hackclub/ari` SDK sends the legacy body-only signature, so the seeded programme allows it. New senders should send `X-Ari-Timestamp` (Unix seconds) and sign `timestamp.body`; timestamps older than five minutes are rejected.

## Development

```sh
go test ./...
go vet ./...
golangci-lint run ./...
cd web && bun run svelte:check && bun run build
wt contract --dir .   # after changing web/src/lib/contract.ts
wt build --dir .
```

The contract in [`web/src/lib/contract.ts`](./web/src/lib/contract.ts) is the source of truth for every request and response. `wt contract` writes the JSON schemas and generated Go types; both are tracked so a clean checkout builds without running the generator.

The app builds against the v2 SDK (`github.com/heliodex/better-deploy/wt/packages/v2`). Storage, HTTP, OAuth, sessions, durable jobs, webhook signing and the signed outbox all come from shared packages; `packages/v2/sign` verifies inbound Ari signatures and `packages/v2/outbox` owns delivery leases, retries, permanent failures and the stored attempt history.

### What is deliberately fake

- **Checks** are real for public GitHub repositories on new ingests and run as a durable job. Hackatime time and journal/clip coverage are checked. Fixture ships carry prescribed results. GitLab, demo probes, identity and duplicate checks are not ported yet.
- **Auth** uses Hack Club Auth over OIDC when configured. Development accounts remain for local work; the platform identity provider is not used because it is a separate password store, and there is no roster admin UI yet.
- **Stage 2** has the queue, confirm, override with a reason, send-back and a final decision. The configurable rule engine, fraud, and admin revert or requeue are later.
- **Delivery receiver** defaults to the in-process `fake://receiver`. Point `REVIEW_OUTBOUND_URL` at a real endpoint to use HTTP; `packages/v2/outbox` signs the exact bytes, leases each attempt, retries 5xx/timeouts and fails permanent 4xx responses.
- **Evidence** for GitHub ships now includes the README, file tree and commits fetched at ingest. Journals and clips still come only from the ingest payload, and the README is shown as plain text rather than rendered Markdown.

## Where the plan goes next

The next work is tracked in [`plan/todo.md`](./plan/todo.md): the remaining check groups, GitLab, the staging webhook cutover, and the remaining evidence work. The contract and schema here are the small stable core those phases extend.
