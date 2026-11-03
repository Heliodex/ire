# Half Life Review

A stage 1 review queue for Half Life hardware ships. This is the first vertical slice of the plan in [`plan/`](./plan): a real signed ingest, a real SQLite queue, claims, decisions and a signed outbox. Checks, reviewer auth and stage 2 are deliberately faked so the review loop can be demonstrated now.

## What works

- Ari-compatible ingest at `POST /api/ingest/{program}` with HMAC over the raw body, timestamp support, replay protection and `409 already_queued`.
- Ari-compatible status at `GET /api/ingest/{program}/status`, withdraw, and the native `reviews?since` / `events?since` recovery feeds.
- A stage 1 queue with claims, heartbeats, drafts, warning acknowledgements, blocking checks, decisions and idempotency keys.
- A stage 2 hold policy for approvals: a warning, a held tier, or hours above the claim waits for the second pass and emits no webhook. Changes and rejections are final and always deliver. The stage 1 endpoints refuse a ship that is already on the second pass.
- A signed outbox written in the decision transaction, an in-process receiver that verifies `timestamp.delivery_id.body`, retries on a schedule and keeps the exact payload.
- Three fixture ships extracted from real `.projects.csv` rows: clean, warned and blocked.

## Run it

```sh
wt dev --dir . --port 8080
```

Open `http://review.localhost:8080` and sign in with one of the seeded accounts:

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
4. **Blocked ship** – open `Smart Alarm Clock`; approve is disabled by the repository check. Request changes or reject with a note.
5. **Recovery** – `GET /api/v1/reviews/half-life?since=...` returns the same payload the webhook delivered; `GET /api/ingest/half-life/status?external_id=fix-clean:design` shows `reviewed` with the approved hours.

Reset the demo at any time with `POST /api/v1/dev/reset` while signed in. It deletes the ships and reseeds the three fixtures without touching the programme or the reviewer accounts.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `REVIEW_INGEST_SECRET` | `dev-ingest-secret` | Verifies Ari inbound signatures and the status bearer |
| `REVIEW_OUTBOUND_SECRET` | `dev-outbound-secret` | Signs outbound webhooks |
| `REVIEW_OUTBOUND_URL` | `fake://receiver` | `fake://receiver` keeps deliveries in-process; an `https://` URL posts them |
| `REVIEW_SEED_FIXTURES` | `1` | `0` starts with an empty queue |
| `REVIEW_DEV_AUTH` | `1` | `0` disables the development sign-in endpoint |

Set real secrets with `wt secrets set review REVIEW_INGEST_SECRET` and `REVIEW_OUTBOUND_SECRET` before deploying. **The development login is not Hack Club Auth.** It exists so the queue can be reviewed before HCA credentials arrive, and must be replaced before a real programme uses it.

## API

| Method | Path | Auth |
| --- | --- | --- |
| `POST` | `/api/ingest/{program}` | HMAC `X-Ari-Signature`, optional timestamp and delivery id |
| `POST` | `/api/ingest/{program}/withdraw` | HMAC |
| `GET` | `/api/ingest/{program}/status?external_id=...` | Bearer ingest secret |
| `GET` | `/api/v1/reviews/{program}?since=...` | Bearer ingest secret |
| `GET` | `/api/v1/events/{program}?since=...` | Bearer ingest secret |
| `POST` | `/api/v1/dev/login` | Development only |
| `GET` | `/api/v1/programs/{program}/queue` | Session |
| `GET` | `/api/v1/programs/{program}/submissions/{id}` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/claim` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/heartbeat` | Session |
| `DELETE` | `/api/v1/programs/{program}/submissions/{id}/claim` | Session |
| `PUT` | `/api/v1/programs/{program}/submissions/{id}/draft` | Session |
| `POST` | `/api/v1/programs/{program}/submissions/{id}/decision` | Session |
| `GET` | `/api/v1/programs/{program}/outbox` | Session |
| `POST` | `/jobs/tick` | Supervisor schedule |

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

### What is deliberately fake

- **Checks** are local and payload-only (`localChecks`). The fixture ships carry prescribed results that show what the ported repo checks will report. No network fetch, forge token or Hackatime call happens yet.
- **Auth** is the dev account table, not HCA.
- **Stage 2** is a state and a policy, not a screen. A held decision waits in `stage2_hold` and emits no webhook. Fraud has a configured minimum stage of 2 and no implementation.
- **Delivery receiver** is the in-process `fake://receiver`, which verifies the same HMAC the real handler will. Point `REVIEW_OUTBOUND_URL` at a real endpoint to exercise HTTP delivery.
- **Evidence** is what Half Life sends in the ingest payload; Review does not fetch journals, commits or clips itself yet.

## Where the plan goes next

The order in [`plan/07-delivery.md`](./plan/07-delivery.md) still holds: port the repo/commit/evidence checks, then Stage 2 with the fraud provider, then admin and analytics. The contract and schema here are the small stable core those phases extend.
