<script lang="ts">
import { program } from "../../lib/api"
import { session } from "../../lib/session.svelte.js"

const endpoints = [
	{
		auth: "HMAC",
		method: "POST",
		path: `/api/ingest/${program}`,
		purpose: "Create a ship, or a new version after a withdraw or resubmission.",
	},
	{
		auth: "HMAC",
		method: "POST",
		path: `/api/ingest/${program}/withdraw`,
		purpose: "Close the open ship for an external id. Idempotent.",
	},
	{
		auth: "Bearer",
		method: "GET",
		path: `/api/ingest/${program}/status?external_id=…`,
		purpose: "Current phase, version, checks and standing decision.",
	},
	{
		auth: "Bearer",
		method: "GET",
		path: `/api/v1/programs/${program}/parity`,
		purpose: "Open queue with hours, BOM total and check summaries for the parity script.",
	},
	{
		auth: "Bearer",
		method: "GET",
		path: `/api/v1/reviews/${program}?since=…`,
		purpose: "Final outbound events after a cursor, for reconciliation.",
	},
]

const phases = [
	["processing", "checking", "Checks are running."],
	["review", "ready / claimed", "Waiting for a reviewer, or claimed by one."],
	["under_review", "in_review", "Open in a reviewer's session."],
	["needs_attention", "needs_attention", "A blocking check failed."],
	["second_pass", "stage2_hold", "Held for an organiser's second pass."],
	["reviewed", "decided", "A final decision exists."],
	["withdrawn", "withdrawn", "Closed without a decision."],
]

const errors = [
	["401 bad_signature", "The HMAC, timestamp or secret is wrong."],
	["404 unknown ship", "No submission exists for that id or external id."],
	["409 already_queued", "An open ship already exists; the body names its id and version."],
	["409 shadow_mode", "This deployment is read-only; decisions stay in Ari."],
	["413", "The body is larger than the 16 MiB ingest cap."],
	["422 invalid", "A required field is missing or malformed; the body names the field."],
]

const canCancel = $derived(session.me !== null)
</script>

<svelte:head>
	<title>Integration docs · Half Life Review</title>
	<meta content="Review ingest, status, parity and outbound integration guide" name="description">
</svelte:head>

<div class="docs">
	<section class="docs-head">
		<div>
			<p class="crumbs"><a href={canCancel ? "/queue" : "/"}>{canCancel ? "Queue" : "Sign in"}</a></p>
			<h1>Integration docs</h1>
			<p class="lead">
				Review accepts Ari-compatible ships, runs its own checks and shows the queue. Half Life can send a
				shadow copy while Ari keeps deciding, then switch ownership later without changing the wire format.
			</p>
		</div>
	</section>

	<div class="callout" role="note">
		<strong>Live and shadow deployments</strong>
		A deployment runs either live or in shadow mode. In shadow mode ships and checks flow, but reviewers cannot
		claim or decide and Review writes no deliveries; the header marks it. In live mode a final decision is written
		to the outbox and delivered with the signature below.
	</div>

	<section class="ui-card padded doc-section">
		<h2>Endpoints</h2>
		<p class="note">Machine endpoints use the programme slug, not the opaque programme id.</p>
		<div class="ui-table">
			<table>
				<thead>
					<tr>
						<th scope="col">Method</th>
						<th scope="col">Path</th>
						<th scope="col">Auth</th>
						<th scope="col">Purpose</th>
					</tr>
				</thead>
				<tbody>
					{#each endpoints as endpoint (endpoint.path)}
						<tr>
							<td><code>{endpoint.method}</code></td>
							<td><code>{endpoint.path}</code></td>
							<td>{endpoint.auth}</td>
							<td>{endpoint.purpose}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>

	<section class="ui-card padded doc-section">
		<h2>Signing an ingest</h2>
		<p>
			Send <code>X-Ari-Timestamp</code> and <code>X-Ari-Signature</code>, where the signature is the lowercase hex
			HMAC-SHA256 of <code>timestamp.body</code> with the programme's ingest secret. Timestamps outside a
			five-minute window are rejected. Send a stable <code>X-Ari-Delivery-Id</code> so a retry of the same body is
			a duplicate rather than a second ship.
		</p>
		<pre class="code">BODY='&#123;"external_id":"proj_abc:design", ...&#125;'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$REVIEW_INGEST_SECRET" -hex | cut -d' ' -f2)

curl -X POST "$REVIEW_API_URL/api/ingest/{program}" \
  -H "Content-Type: application/json" \
  -H "X-Ari-Timestamp: $TS" \
  -H "X-Ari-Signature: $SIG" \
  -H "X-Ari-Delivery-Id: hl-$SUBMISSION_ID" \
  --data-binary "$BODY"</pre>
		<p class="note">
			The legacy body-only signature remains available only where the programme setting allows it. New senders
			should use <code>timestamp.body</code>.
		</p>
	</section>

	<section class="ui-card padded doc-section">
		<h2>What to send</h2>
		<p>
			The required fields are <code>external_id</code>, <code>title</code>, <code>description</code>,
			<code>maker.email</code>
			and <code>maker.name</code>. A hand-in may also carry the repo and demo URLs, a thumbnail, journals,
			evidence links, Hackatime projects and a nested <code>meta</code> block.
		</p>
		<pre class="code">{`{
  "external_id": "proj_abc:design",
  "title": "Starbie",
  "description": "A motion-controlled digital pet on a star-shaped PCB.",
  "repo_url": "https://github.com/maker/starbie",
  "thumbnail_url": "https://cdn.example.com/starbie.png",
  "maker": {
    "email": "maker@example.com",
    "name": "Maker",
    "slack_id": "U012345",
    "hackatime_id": "12345"
  },
  "journals": [
    { "at": "2026-11-01T12:00:00Z", "text": "Schematic and layout", "minutes": 180 }
  ],
  "meta": {
    "phase": "design",
    "program": "half-life",
    "theme": "starbie",
    "week": 4,
    "requested_tier": 2,
    "grant_cap_cents": 10000,
    "submission_notes": "Second revision after the layout review.",
    "hours": {
      "claimed_seconds": 27144,
      "journal_seconds": 27000,
      "hackatime_seconds": 0,
      "timelapse_seconds": 5400
    },
    "bom": [
      { "name": "XIAO ESP32-C3", "qty": 1, "unit_usd": 6.5, "vendor": "Seeed" }
    ],
    "prior_reviews": [
      { "at": "2026-10-20T10:00:00Z", "result": "changes", "reviewer": "reviewer@example.com" }
    ]
  }
}`}</pre>
		<p class="note">
			The nested <code>meta.bom</code>, <code>meta.hours</code> and <code>meta.prior_reviews</code> are the Review
			additions. Flatten or drop them when sending the same ship to Ari.
		</p>
	</section>

	<section class="ui-card padded doc-section">
		<h2>Responses and phases</h2>
		<p>
			A create returns <code>201</code> with <code>&#123; id, status, version &#125;</code>; a repeated delivery
			returns
			<code>200</code>
			with <code>status: "duplicate"</code>. An open ship returns
			<code>409 &#123; error: "already_queued", id, version &#125;</code>. Withdraw returns
			<code>&#123; id, status: "withdrawn" &#125;</code>
			and is idempotent.
		</p>
		<div class="ui-table">
			<table>
				<thead>
					<tr>
						<th scope="col">Ari phase</th>
						<th scope="col">Review status</th>
						<th scope="col">Meaning</th>
					</tr>
				</thead>
				<tbody>
					{#each phases as [phase, status, meaning] (phase)}
						<tr>
							<td><code>{phase}</code></td>
							<td><code>{status}</code></td>
							<td>{meaning}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>

	<section class="ui-card padded doc-section">
		<h2>Parity and reconciliation</h2>
		<p>
			The parity feed returns every open ship with the external id, version, claimed and approved seconds, BOM
			total, check summary and phase. It is read-only and answers the same question Half Life's
			<code>review:parity</code>
			script asks, so the two queues can be compared without applying anything.
		</p>
		<p>
			After a live decision, <code>GET /api/v1/reviews/{program}?since=…</code> pages through the final outbound
			events. The response carries <code>next_cursor</code>; apply each event idempotently and store its time as
			the next <code>since</code>.
		</p>
	</section>

	<section class="ui-card padded doc-section">
		<h2>Outbound events</h2>
		<p>
			In live mode Review posts <code>review.approved</code>, <code>review.changes</code> or
			<code>review.rejected</code>
			to the programme's outbound URL. The signature covers
			<code>timestamp.delivery_id.body</code>
			with the outbound secret, and the same envelope carries
			<code>X-Ari-Timestamp</code>, <code>X-Ari-Signature</code> and <code>X-Ari-Delivery-Id</code>. Held
			second-pass decisions write no event until the final decision.
		</p>
		<p class="note">
			Retries use a 30 second to 24 hour ladder. A 4xx is permanent. The deliveries page shows stored events and
			the in-process receiver's verified bodies.
		</p>
	</section>

	<section class="ui-card padded doc-section">
		<h2>Half Life configuration</h2>
		<p>Keep Ari untouched and add a separate Review provider:</p>
		<pre class="code">REVIEW_API_URL=https://review-wt-demo.k.hackclub.dev
REVIEW_PROGRAM=half-life
REVIEW_SIGNING_SECRET=&lt;Review's REVIEW_INGEST_SECRET&gt;
REVIEW_MODE=shadow
REVIEW_BASE=https://review-wt-demo.k.hackclub.dev

# Review itself
REVIEW_SHADOW=1
REVIEW_INGEST_SECRET=&lt;same secret&gt;
REVIEW_OUTBOUND_URL=fake://receiver
REVIEW_SEED_FIXTURES=0
REVIEW_DEV_AUTH=0</pre>
		<p>
			<code>REVIEW_MODE=shadow</code>
			labels Half Life's side only. Review's own
			<code>REVIEW_SHADOW=1</code>
			is what makes the API read-only and suppresses deliveries.
		</p>
	</section>

	<section class="ui-card padded doc-section">
		<h2>Troubleshooting</h2>
		<div class="ui-table">
			<table>
				<thead>
					<tr>
						<th scope="col">Response</th>
						<th scope="col">What to check</th>
					</tr>
				</thead>
				<tbody>
					{#each errors as [error, fix] (error)}
						<tr>
							<td><code>{error}</code></td>
							<td>{fix}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<p class="note">
			If a create returns <code>already_queued</code> for a ship you thought was closed, withdraw the old external
			id and retry once. A stale copy after a resubmission is the usual cause.
		</p>
	</section>
</div>

<style>
.docs {
	width: 100%;
	max-width: 920px;
}

.docs-head {
	padding-bottom: var(--space-4);
}

.crumbs {
	font-size: var(--text-sm);
	color: var(--text-3);
}

.crumbs a {
	color: var(--text-2);
	text-decoration: none;
}

.crumbs a:hover {
	color: var(--text);
}

h1 {
	font-size: var(--text-2xl);
}

.lead {
	max-width: 62ch;
	padding-top: var(--space-2);
	font-size: var(--text-sm);
	line-height: 1.6;
	color: var(--text-2);
}

.callout {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: var(--space-3) var(--space-4);
	margin-bottom: var(--space-4);
	font-size: var(--text-sm);
	line-height: 1.5;
	color: var(--text-2);
	background: var(--surface-2);
	border: 1px solid var(--border);
	border-radius: var(--radius-md);
}

.callout strong {
	color: var(--text);
}

.doc-section {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	max-width: 100%;
	margin-bottom: var(--space-4);
}

.doc-section h2 {
	font-size: var(--text-lg);
}

.doc-section p {
	max-width: 70ch;
	font-size: var(--text-sm);
	line-height: 1.6;
	color: var(--text-2);
}

.doc-section code {
	padding: 0.1em 0.3em;
	font-family: var(--font-mono);
	font-size: 0.92em;
	color: var(--text);
	background: var(--code-background);
	border-radius: 4px;
}

.code {
	padding: var(--space-3);
	overflow-x: auto;
	font-family: var(--font-mono);
	font-size: var(--text-xs);
	line-height: 1.6;
	color: var(--code-foreground);
	white-space: pre;
	background: var(--code-background);
	border: 1px solid var(--border);
	border-radius: var(--radius-md);
}

.note {
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
