<script lang="ts">
import LoadingState from "../LoadingState.svelte"

let { name, text }: { name: string; text: string } = $props()

let host = $state<HTMLDivElement | null>(null)
let error = $state("")
let loading = $state(true)

// loadKicanvas injects the vendored KiCanvas bundle once per page, then resolves when its elements are registered.
async function loadKicanvas(): Promise<void> {
	if (customElements.get("kicanvas-embed")) return

	const pending = globalThis as unknown as { __kicanvasLoading?: Promise<void> }
	try {
		pending.__kicanvasLoading ??= waitForKicanvas()
		await pending.__kicanvasLoading
	} catch (err: unknown) {
		delete pending.__kicanvasLoading
		throw err
	}
}

// waitForKicanvas injects the vendored viewer bundle and resolves once its elements are registered.
function waitForKicanvas(): Promise<void> {
	const { promise, resolve, reject } = Promise.withResolvers<void>()
	const script = document.createElement("script")
	script.type = "module"
	script.src = "/vendor/kicanvas.js"
	script.addEventListener("load", (): void => resolve())
	script.addEventListener("error", (): void => reject(new Error("Board viewer failed to load")))
	document.head.append(script)

	return promise
}

// isKicanvasError reports whether a window error came from the board renderer.
function isKicanvasError(event: ErrorEvent): boolean {
	return /kicanvas/i.test(`${event.message ?? ""} ${event.filename ?? ""}`)
}

// onError turns a renderer failure into a readable notice instead of a blank panel.
function onError(event: ErrorEvent): void {
	if (!host?.isConnected) return
	if (!isKicanvasError(event)) return

	event.preventDefault()
	error = "This file could not be rendered."
	loading = false
}

// mountEmbed registers the KiCanvas elements on first use, then renders this board from its text.
$effect((): (() => void) => {
	const element = host
	if (!element) return (): void => {}

	window.addEventListener("error", onError)

	void (async (): Promise<void> => {
		try {
			await loadKicanvas()
			if (!element.isConnected) return

			const embed = document.createElement("kicanvas-embed")
			embed.setAttribute("controls", "full")

			const source = document.createElement("kicanvas-source")
			source.setAttribute("name", name)
			source.textContent = text
			embed.append(source)
			element.append(embed)
			loading = false
		} catch {
			if (!element.isConnected) return

			error = "The file viewer could not be loaded."
			loading = false
		}
	})()

	return (): void => {
		window.removeEventListener("error", onError)
		element.replaceChildren()
	}
})
</script>

<div class="preview">
	{#if loading}
		<LoadingState message="Rendering file…" />
	{:else if error}
		<p class="ui-notice" data-tone="bad" role="alert">{error}</p>
	{/if}
	<div class="board" bind:this={host}></div>
</div>

<style>
.preview {
	margin-top: var(--space-3);
}

.board :global(kicanvas-embed) {
	display: block;
	width: 100%;
	height: 480px;
	border: 1px solid var(--border);
	border-radius: var(--radius-md);
}

@media (max-width: 640px) {
	.board :global(kicanvas-embed) {
		height: 320px;
	}
}
</style>
