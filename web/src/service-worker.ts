/// <reference lib="webworker" />

// SvelteKit excludes the service worker from the app tsconfig because it has its own global scope.
// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- the file targets the service worker global, not Window
const worker = self as unknown as ServiceWorkerGlobalScope

const cacheName = "project-covers-v1"
const maxEntries = 400
const cachePrefix = "project-covers-"

// cachesProjectCover keeps project images across navigations, where the asset host does not
// reliably reuse the browser HTTP cache. Only the cover host and local fixture images are matched.
function cachesProjectCover(url: URL): boolean {
	if (url.origin === worker.location.origin) return url.pathname.startsWith("/projects/")

	return (
		url.protocol === "https:"
		&& (url.hostname === "hackclub-assets.com" || url.hostname.endsWith(".hackclub-assets.com"))
	)
}

async function trim(cache: Cache): Promise<void> {
	const keys = await cache.keys()
	await Promise.all(keys.slice(0, Math.max(0, keys.length - maxEntries)).map(key => cache.delete(key)))
}

async function respondWithCache(request: Request): Promise<Response> {
	const cache = await caches.open(cacheName)
	const cached = await cache.match(request)
	if (cached) return cached

	const response = await fetch(request)
	if (!response.ok && response.type !== "opaque") return response

	try {
		await cache.put(request, response.clone())
		await trim(cache)
	} catch {
		// A single opaque or oversized response is not worth failing the image over.
	}

	return response
}

worker.addEventListener("install", (): void => {
	void worker.skipWaiting()
})

worker.addEventListener("activate", (event: ExtendableEvent): void => {
	event.waitUntil(
		(async () => {
			const names = await caches.keys()
			await Promise.all(
				names
					.filter(name => name.startsWith(cachePrefix) && name !== cacheName)
					.map(name => caches.delete(name)),
			)
			await worker.clients.claim()
		})(),
	)
})

worker.addEventListener("fetch", (event: FetchEvent): void => {
	const url = new URL(event.request.url)
	if (event.request.destination !== "image" || !cachesProjectCover(url)) return

	event.respondWith(respondWithCache(event.request))
})
