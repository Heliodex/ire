<script lang="ts">
import type { Note } from "../../api"
import { maxNote } from "../../contract"
import { formatDateTime } from "../../format"

interface Props {
	canDelete(note: Note): boolean
	notes: Note[]
	onAdd(subjectType: string, body: string): Promise<void>
	onRemove(note: Note): Promise<void>
}

let { canDelete, notes, onAdd, onRemove }: Props = $props()

const SUBJECT_LABELS: { [_: string]: string } = {
	maker: "Maker",
	project: "Project",
	submission: "This ship",
}

let subject = $state("submission")
let body = $state("")
let saving = $state(false)
let error = $state("")

// subjectLabel names the scope a note persists across.
function subjectLabel(value: string): string {
	return SUBJECT_LABELS[value] ?? value
}

// submit stores the note and clears the form.
async function submit(): Promise<void> {
	if (body.trim() === "") return

	saving = true
	error = ""

	try {
		await onAdd(subject, body.trim())
		body = ""
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not save this note."
	} finally {
		saving = false
	}
}
</script>

<div class="notes">
	{#if notes.length === 0}
		<p class="hint">No notes yet. Notes are internal and never shown to the maker.</p>
	{:else}
		<ul class="note-list">
			{#each notes as note (note.id)}
				<li>
					<div class="note-head">
						<span class="ui-badge">{subjectLabel(note.subjectType)}</span>
						<strong>{note.authorName ?? note.authorEmail}</strong>
						<small>{formatDateTime(note.createdAt)}</small>
						{#if canDelete(note)}
							<button
								class="link note-remove"
								onclick={(): Promise<void> => onRemove(note)}
								type="button"
							>
								Remove
							</button>
						{/if}
					</div>
					<p>{note.body}</p>
				</li>
			{/each}
		</ul>
	{/if}

	<form
		class="note-form"
		onsubmit={(event): void => {
			event.preventDefault()
			void submit()
		}}
	>
		<label class="field">
			<span>On</span>
			<select class="ui-input" bind:value={subject}>
				<option value="submission">This ship</option>
				<option value="project">The project</option>
				<option value="maker">The maker</option>
			</select>
		</label>
		<label class="field note-body">
			<span>Note <span class="hint">{body.length} / {maxNote}</span></span>
			<textarea
				aria-label="Internal note"
				class="ui-input"
				maxlength={maxNote}
				placeholder="Context for the next reviewer. Never shown to the maker."
				rows="3"
				bind:value={body}
			></textarea>
		</label>
		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}
		<button class="ui-button primary" disabled={saving || body.trim() === ""} type="submit">
			{saving ? "Saving…" : "Add note"}
		</button>
	</form>
</div>

<style>
.notes {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.note-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: 0;
	margin: 0;
	list-style: none;
}

.note-list li {
	padding: var(--space-2) var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.note-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: baseline;
	font-size: var(--text-sm);
	color: var(--text-2);
}

.note-head small {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.note-head .note-remove {
	margin-left: auto;
	font-size: var(--text-xs);
}

.note-list p {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
	white-space: pre-wrap;
}

.note-form {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: end;
	padding-top: var(--space-2);
}

.note-form .field {
	width: 9rem;
}

.note-form .note-body {
	flex: 1 1 18rem;
	width: auto;
}

.note-form .error {
	flex-basis: 100%;
	font-size: var(--text-xs);
	color: var(--accent-red);
}

.hint {
	font-size: var(--text-xs);
	color: var(--text-3);
}
</style>
