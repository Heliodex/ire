/// <reference lib="webworker" />

// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- service workers use a separate global type
const worker = /** @type {ServiceWorkerGlobalScope} */ (/** @type {unknown} */ (self))

const cacheName = "project-covers-v1"
const maxEntries = 400
const cachePrefix = "project-covers-"

// cachesProjectCover keeps project images across navigations, where the asset host does not
// reliably reuse the browser HTTP cache. Only the cover host and local fixture images are matched.
/** @param {URL} url */
function cachesProjectCover(url) {
	if (url.origin === worker.location.origin) return url.pathname.startsWith("/projects/")

	return (
		url.protocol === "https:"
		&& (url.hostname === "hackclub-assets.com" || url.hostname.endsWith(".hackclub-assets.com"))
	)
}

/** @param {Cache} cache */
async function trim(cache) {
	const keys = await cache.keys()
	for (const key of keys.slice(0, Math.max(0, keys.length - maxEntries))) await cache.delete(key)
}

/** @param {Request} request */
async function respondWithCache(request) {
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

worker.addEventListener("install", () => {
	void worker.skipWaiting()
})

worker.addEventListener("activate", event => {
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

worker.addEventListener("fetch", event => {
	const url = new URL(event.request.url)
	if (event.request.destination !== "image" || !cachesProjectCover(url)) return

	event.respondWith(respondWithCache(event.request))
})
