<script lang="ts">
import { onMount } from "svelte"
import { goto } from "$app/navigation"
import { fetchMe, program } from "../../../lib/api"
import { deleteStation, fetchStation, fetchStations, saveStation } from "../../../lib/api.gen"
import { ApiError } from "../../../lib/api-error"
import LoadingState from "../../../lib/components/LoadingState.svelte"
import { hasPermission, permissionRosterManage, programPermissions } from "../../../lib/permissions"
import { dropSignIn, session } from "../../../lib/session.svelte.js"

type Station = Awaited<ReturnType<typeof fetchStations>>["stations"][number]
type Definition = Parameters<typeof saveStation>[1]
type Question = Definition["questions"][number]

interface Draft {
	active: number
	description: string
	key: string
	label: string
	questions: Question[]
	sort: number
	stage: number
	themes: string
}

let stations = $state<Station[]>([])
let draft = $state<Draft | undefined>(undefined)
let originalKey = $state("")
let loading = $state(true)
let saving = $state(false)
let confirmingDelete = $state(false)
let error = $state("")
let notice = $state("")

const permissions = $derived(programPermissions(session.me, program))
const canEdit = $derived(hasPermission(permissions, permissionRosterManage))

onMount((): void => {
	void boot()
})

async function boot(): Promise<void> {
	try {
		session.me ??= await fetchMe()
	} catch {
		void goto("/")
		return
	}

	if (!canEdit) {
		loading = false
		return
	}

	await refresh()
}

async function refresh(): Promise<void> {
	try {
		const [one, two] = await Promise.all([fetchStations(1, 1), fetchStations(2, 1)])
		stations = [...one.stations, ...two.stations]
	} catch (err) {
		if (err instanceof ApiError && err.code === "unauthorized") {
			dropSignIn()
			void goto("/")
			return
		}

		error = err instanceof Error ? err.message : "Could not load the stations."
	} finally {
		loading = false
	}
}

// edit loads a station definition as an editable draft.
async function edit(key: string): Promise<void> {
	error = ""
	notice = ""
	confirmingDelete = false

	try {
		const station = await fetchStation(key)
		originalKey = station.key
		draft = {
			active: station.active,
			description: station.description,
			key: station.key,
			label: station.label,
			questions: station.questions.map(question => ({ ...question })),
			sort: station.sort,
			stage: station.stage,
			themes: station.themes.join(", "),
		}
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not load that station."
	}
}

function create(): void {
	originalKey = ""
	confirmingDelete = false
	draft = {
		active: 1,
		description: "",
		key: "",
		label: "",
		questions: [{ evidenceKind: "text", key: "", prompt: "", required: 1, sort: 0 }],
		sort: (stations.at(-1)?.sort ?? 0) + 10,
		stage: 1,
		themes: "",
	}
}

function addQuestion(): void {
	if (draft === undefined) return

	draft.questions = [
		...draft.questions,
		{ evidenceKind: "text", key: "", prompt: "", required: 1, sort: draft.questions.length },
	]
}

function removeQuestion(index: number): void {
	if (draft === undefined) return

	draft.questions = draft.questions.filter((_, position) => position !== index)
}

async function save(): Promise<void> {
	if (draft === undefined) return

	saving = true
	error = ""
	notice = ""

	try {
		const definition: Definition = {
			active: draft.active ? 1 : 0,
			description: draft.description,
			key: draft.key.trim(),
			label: draft.label.trim(),
			questions: draft.questions.map((question, index) => ({
				...question,
				key: question.key.trim(),
				prompt: question.prompt.trim(),
				required: question.required ? 1 : 0,
				sort: index,
			})),
			sort: draft.sort,
			stage: draft.stage,
			themes: draft.themes
				.split(",")
				.map(theme => theme.trim())
				.filter(theme => theme !== ""),
		}

		await saveStation(definition.key, definition)
		notice = `Saved ${definition.label}.`
		originalKey = definition.key
		await refresh()
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not save the station."
	} finally {
		saving = false
	}
}

async function remove(key: string): Promise<void> {
	if (!confirmingDelete) {
		confirmingDelete = true
		return
	}

	confirmingDelete = false
	saving = true
	error = ""
	notice = ""

	try {
		await deleteStation(key)
		notice = `Deleted ${key}.`
		draft = undefined
		originalKey = ""
		await refresh()
	} catch (err) {
		error = err instanceof Error ? err.message : "Could not delete the station."
	} finally {
		saving = false
	}
}
</script>

<svelte:head>
	<title>Stations · Admin · Ire</title>
</svelte:head>

<section class="page-head">
	<div>
		<p class="crumbs"><a href="/admin">Admin</a></p>
		<h1>Stations</h1>
		<p>Question sets shared by every stage. Changes apply to new reviews immediately.</p>
	</div>
	<button class="ui-button" data-variant="outline" onclick={create} type="button">New station</button>
</section>

{#if error}
	<p class="ui-notice" data-tone="bad" role="alert">{error}</p>
{/if}
{#if notice}
	<p class="ui-notice" data-tone="good" role="status">{notice}</p>
{/if}

{#if !loading && !canEdit}
	<div class="ui-card ui-empty">
		<strong>Admins only</strong>
		<span>Editing station questions needs the roster manager permission.</span>
	</div>
{:else if loading}
	<div class="ui-card"><LoadingState message="Loading stations…" /></div>
{:else}
	<div class="layout">
		<aside class="ui-card list">
			{#each [
				{ label: "First pass", stage: 1 },
				{ label: "Second pass", stage: 2 },
			] as group (group.stage)}
				<h2>{group.label}</h2>
				{#each stations.filter(station => station.stage === group.stage) as station (station.key)}
					<button
						class="row"
						onclick={(): Promise<void> => edit(station.key)}
						type="button"
						class:active={draft?.key === station.key}
					>
						<span>{station.label}</span>
						<small>
							{station.key}
							· {station.count} waiting{station.active === 0 ? " · inactive" : ""}
						</small>
					</button>
				{/each}
			{/each}
		</aside>

		{#if draft}
			<form
				class="ui-card padded editor"
				onsubmit={(event): void => {
					event.preventDefault()
					void save()
				}}
			>
				<div class="fields">
					<label class="field">
						<span>Key</span>
						<input class="ui-input" disabled={originalKey !== ""} required bind:value={draft.key}>
					</label>
					<label class="field">
						<span>Label</span>
						<input class="ui-input" required bind:value={draft.label}>
					</label>
					<label class="field">
						<span>Stage</span>
						<select class="ui-input" bind:value={draft.stage}>
							<option value={1}>First pass</option>
							<option value={2}>Second pass</option>
						</select>
					</label>
					<label class="field">
						<span>Sort</span>
						<input class="ui-input" min="0" type="number" bind:value={draft.sort}>
					</label>
					<label class="field">
						<span>Themes</span>
						<input class="ui-input" placeholder="pcb, design (blank for all)" bind:value={draft.themes}>
					</label>
					<label class="field checkbox">
						<input type="checkbox" bind:checked={draft.active}>
						<span>Active</span>
					</label>
				</div>

				<label class="field">
					<span>Description</span>
					<textarea class="ui-input" rows="2" bind:value={draft.description}></textarea>
				</label>

				<div class="questions-head">
					<h2>Questions</h2>
					<button class="ui-button sm" data-variant="outline" onclick={addQuestion} type="button">
						Add question
					</button>
				</div>

				{#each draft.questions as question, index (index)}
					<div class="question">
						<div class="fields">
							<label class="field">
								<span>Key</span>
								<input class="ui-input" required bind:value={question.key}>
							</label>
							<label class="field">
								<span>Sort</span>
								<input class="ui-input" min="0" type="number" bind:value={question.sort}>
							</label>
							<label class="field">
								<span>Evidence</span>
								<select class="ui-input" bind:value={question.evidenceKind}>
									<option value="text">Text</option>
									<option value="image">Image</option>
									<option value="time">Time</option>
								</select>
							</label>
							<label class="field checkbox">
								<input type="checkbox" bind:checked={question.required}>
								<span>Required</span>
							</label>
						</div>
						<label class="field">
							<span>Prompt</span>
							<textarea class="ui-input" required rows="2" bind:value={question.prompt}></textarea>
						</label>
						<button class="link danger" onclick={(): void => removeQuestion(index)} type="button">
							Remove question
						</button>
					</div>
				{/each}

				<div class="actions">
					<button class="ui-button primary" disabled={saving} type="submit">
						{saving ? "Saving…" : "Save station"}
					</button>
					{#if originalKey !== ""}
						<button
							class="ui-button danger"
							disabled={saving}
							onclick={(): Promise<void> => remove(originalKey)}
							title={confirmingDelete ? `Deletes ${originalKey} and its answers` : undefined}
							type="button"
						>
							{confirmingDelete ? "Confirm delete" : "Delete station"}
						</button>
					{/if}
				</div>
			</form>
		{:else}
			<div class="ui-card ui-empty">
				<strong>Pick a station</strong>
				<span>Select a station to edit it, or create a new one.</span>
			</div>
		{/if}
	</div>
{/if}

<style>
.page-head {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-3);
	align-items: flex-end;
	justify-content: space-between;
	padding-bottom: var(--space-4);
}

.crumbs {
	font-size: var(--text-sm);
	color: var(--text-3);
}

h1 {
	font-size: var(--text-2xl);
}

.page-head p {
	padding-top: var(--space-1);
	font-size: var(--text-sm);
	color: var(--text-2);
}

.layout {
	display: grid;
	grid-template-columns: minmax(15rem, 20rem) minmax(0, 1fr);
	gap: var(--space-3);
	align-items: start;
}

.list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: var(--space-2);
}

.list h2 {
	padding: var(--space-2);
	font-size: var(--text-sm);
	color: var(--text-3);
}

.row {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	padding: var(--space-2);
	text-align: left;
	cursor: pointer;
	background: none;
	border: 0;
	border-radius: var(--radius-sm);
}

.row:hover,
.row.active {
	background: var(--surface-2);
}

.row small {
	font-size: var(--text-xs);
	color: var(--text-3);
}

.editor {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
}

.fields {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
	gap: var(--space-2);
}

.field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	font-size: var(--text-xs);
	font-weight: 700;
	color: var(--text-2);
}

.field.checkbox {
	flex-direction: row;
	align-items: center;
	font-size: var(--text-sm);
}

.questions-head,
.actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	align-items: center;
	justify-content: space-between;
}

.question {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: var(--space-3);
	background: var(--surface-2);
	border-radius: var(--radius-sm);
}

.link {
	padding: 0;
	font-size: var(--text-xs);
	color: var(--text-2);
	cursor: pointer;
	background: none;
	border: 0;
}

.link.danger {
	color: var(--status-changes);
}

@media (max-width: 50rem) {
	.layout {
		grid-template-columns: minmax(0, 1fr);
	}
}
</style>
