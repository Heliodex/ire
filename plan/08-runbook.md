# 08 – Pilot, cutover and rollback runbook

This runbook covers the review service as deployed through `wt`. Half Life stays the only writer of credit, tiers, grants, the ledger and maker notifications.

## Before the pilot

1. Set the production secrets through `wt secrets set`:
   - `REVIEW_INGEST_SECRET` – the same value Half Life signs ingest with.
   - `REVIEW_OUTBOUND_SECRET` – the value the staging receiver verifies.
   - `REVIEW_OUTBOUND_URL` – the staging receiver URL.
   - `HC_CLIENT_ID`, `HC_CLIENT_SECRET`, `HC_REDIRECT_URI` – the registered Hack Club Auth application.
2. Set `REVIEW_OUTBOUND_DRY_RUN=1` for the shadow window. Deliveries appear as `delivered` in `/deliveries` with their payloads, but nothing leaves the service.
3. Set `REVIEW_SHADOW=1` and keep `REVIEW_OUTBOUND_URL=fake://receiver` for the read-only shadow window. Reviewer claim, draft, decision and send-back calls return `409 shadow_mode`, and no outbox row is written.
4. Set `REVIEW_DEV_AUTH=0` and remove the seeded accounts from the production copy:

   ```sql
   delete from user
   where email in ('reviewer@example.com', 'organiser@example.com', 'admin@example.com');
   ```

   The memberships cascade with the user rows. Re-run the bootstrap only if the roster is empty.
5. Sign in once with the bootstrap admin, invite the real reviewers and organizers at `/admin`, then disable the bootstrap by setting `REVIEW_BOOTSTRAP_EMAIL` to an address that cannot sign in, or by removing the secret reference.
6. Run `wt doctor review` and open `/queue`, `/review/{id}` and `/admin`.

## Shadow window

- Compare the dry-run deliveries in `/deliveries` and `GET /api/v1/programs/half-life/outbox` with what Half Life would have applied.
- Watch `GET /metrics` with the ingest bearer token: `review_queue_open_total`, `review_hold_rate`, `review_time_to_first_review_seconds`, `review_check_duration_seconds`, `review_delivery_total{status}`.
- Do not flip `REVIEW_OUTBOUND_DRY_RUN` until at least one approved, one changes and one rejected event has been compared by hand.

## Cutover

1. Set `REVIEW_OUTBOUND_DRY_RUN=0` and redeploy.
2. Send the three staging events: approve a fixture, request changes on one and reject one.
3. Confirm the receiver applied each exactly once. The delivery id is stable (`X-Ari-Delivery-Id`), so a retry is safe.
4. Check `GET /api/ingest/half-life/status?external_id=…` and the native recovery feeds before moving live traffic.

## Recovery feeds

- Reviews since a cursor: `GET /api/v1/programs/half-life/reviews?since=2026-11-01T00:00:00Z&limit=100` with `Authorization: Bearer $REVIEW_INGEST_SECRET`.
- Events since a cursor: `GET /api/v1/programs/half-life/events?since=…&limit=100`.
- Both return a `cursor` you can feed back as `since`; the order is stable on `created_at` and id.
- Replaying is read-only: feeds never write to Half Life.

## Rollback

1. Set `REVIEW_OUTBOUND_DRY_RUN=1` first, so a retry cannot double-apply a decision.
2. `wt rollback review <deployment-id>` restores the previous build.
3. Replay Half Life's own state from its ledger; Review is a cache of decisions, not the source of credit.
4. Sessions survive a rollback; revoke a compromised member from `/admin` if needed.

## Incident checks

- A delivery stuck in `pending` with increasing attempts: inspect `last_error` and `last_http_status` in `/deliveries`; 4xx marks `failed` permanently, 5xx and dropped connections retry.
- A check stuck in `checking`: the scheduled tick moves it to `needs_attention` after ten minutes; an organizer can rerun checks from the review rail.
- A repository moved after its snapshot: the review rail shows the stale notice and offers a rerun.
- A session or audit incident: `/admin` lists recent actions; the tick prunes sessions, login flows, old deliveries, closed-ship snapshots and audit rows on fixed windows.

## Staging deployment

The read-only staging instance runs on the shared `wt-demo` supervisor:

- Supervisor: `https://wt-demo.k.hackclub.dev`
- App host: `https://review-wt-demo.k.hackclub.dev`
- App name: `review`
- Ingress: `review-wt-demo.k.hackclub.dev` on the `wt-demo` deployment, service port 8080

Current staging posture:

- `REVIEW_SHADOW=1` and outbound `fake://receiver`
- `REVIEW_SEED_FIXTURES=0` and `REVIEW_DEV_AUTH=0`, so the queue starts empty and only Hack Club Auth can sign in
- Secrets: `HC_CLIENT_ID`, `HC_CLIENT_SECRET`, `HC_REDIRECT_URI`, `REVIEW_BOOTSTRAP_EMAIL`, `REVIEW_INGEST_SECRET`, `REVIEW_OUTBOUND_SECRET`
- The Hack Club Auth application must allow `https://review-wt-demo.k.hackclub.dev/auth/callback`; the deployed client sends that exact URI
- The bootstrap email is the first admin; invite the rest at `/admin` after signing in

Deploy from a clean `main` with the staging env temporarily applied to `app.json`:

```json
"REVIEW_SHADOW": "1",
"REVIEW_SEED_FIXTURES": "0",
"REVIEW_DEV_AUTH": "0",
"REVIEW_OUTBOUND_URL": "fake://receiver",
"REVIEW_INGEST_SECRET": "secret://REVIEW_INGEST_SECRET",
"REVIEW_OUTBOUND_SECRET": "secret://REVIEW_OUTBOUND_SECRET"
```

Then:

```sh
wt deploy --dir . \
  --host https://wt-demo.k.hackclub.dev \
  --app-host review-wt-demo.k.hackclub.dev
```

The first deploy needed `--skip-route-check` because the ingress existed before the app did; later deploys do not. `wt route check review` and `wt doctor review` should both pass.

Do not deploy the development `app.json` unchanged: it defaults to `REVIEW_SHADOW=0`, fixtures on and development sign-in on, which would turn the public staging host into an open demo. The staging overrides above must be applied for every deploy until a separate production manifest exists.

The first staging smoke used the committed Half Life client and the staging ingest secret: design create v1, same-submission replay duplicate v1, build create v1, design withdraw, design recreate v2, and parity with both ships. Both ships were withdrawn; parity is empty again. The fake repository produced the expected check warnings and errors. Hack Club Auth still rejects the staging callback until the redirect URI above is added to the application.

Half Life needs `REVIEW_SIGNING_SECRET` equal to Review's `REVIEW_INGEST_SECRET`, `REVIEW_PROGRAM=half-life`, `REVIEW_API_URL=https://review-wt-demo.k.hackclub.dev` and `REVIEW_MODE=shadow`. Deliver the ingest secret out of band; `wt secrets` lists names only.
