/// <reference lib="webworker" />
/// <reference types="@sveltejs/kit" />

import { version } from "$app/environment"
import { assets, immutable, prerendered } from "$app/manifest"

// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- the file targets the service worker global, not Window
const worker = self as unknown as ServiceWorkerGlobalScope

const appCache = `ire-app-${version}`
const coverCache = "project-covers-v1"
const maxCoverEntries = 400
const cachePrefix = "project-covers-"

const appAssets: string[] = assets.filter((path: string) => !path.startsWith("/projects/"))
const staticSet = new Set<string>(appAssets)
const immutableSet = new Set<string>(immutable)
const precachePaths = [...new Set<string>([...immutable, ...appAssets, ...prerendered])]

function isApi(url: URL): boolean {
	return url.pathname.startsWith("/api/") || url.pathname.startsWith("/v1/")
}

function cachesProjectCover(url: URL): boolean {
	if (url.origin === worker.location.origin) return url.pathname.startsWith("/projects/")
	return (
		url.protocol === "https:"
		&& (url.hostname === "hackclub-assets.com" || url.hostname.endsWith(".hackclub-assets.com"))
	)
}

async function trim(cache: Cache): Promise<void> {
	const keys = await cache.keys()
	await Promise.all(keys.slice(0, Math.max(0, keys.length - maxCoverEntries)).map(key => cache.delete(key)))
}

async function cacheCover(request: Request): Promise<Response> {
	const cache = await caches.open(coverCache)
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

async function cacheFirst(request: Request): Promise<Response> {
	const cache = await caches.open(appCache)
	const cached = await cache.match(request)
	if (cached) return cached
	const response = await fetch(request)
	if (response.ok) await cache.put(request, response.clone())
	return response
}

async function networkFirst(request: Request): Promise<Response> {
	const cache = await caches.open(appCache)
	try {
		const response = await fetch(request)
		if (response.ok) await cache.put(request, response.clone())
		return response
	} catch {
		const cached = await cache.match(request)
		if (cached) return cached
		const fallback = await cache.match("/")
		if (fallback) return fallback
		throw new Error("Offline")
	}
}

worker.addEventListener("install", (event: ExtendableEvent): void => {
	event.waitUntil(
		(async () => {
			const cache = await caches.open(appCache)
			await cache.addAll(precachePaths)
			await worker.skipWaiting()
		})(),
	)
})

worker.addEventListener("activate", (event: ExtendableEvent): void => {
	event.waitUntil(
		(async () => {
			const names = await caches.keys()
			await Promise.all(
				names
					.filter(name => name !== appCache && !(name.startsWith(cachePrefix) && name === coverCache))
					.map(name => caches.delete(name)),
			)
			await worker.clients.claim()
		})(),
	)
})

worker.addEventListener("fetch", (event: FetchEvent): void => {
	if (event.request.method !== "GET") return
	const url = new URL(event.request.url)
	if (url.origin !== worker.location.origin) {
		if (event.request.destination === "image" && cachesProjectCover(url))
			event.respondWith(cacheCover(event.request))
		return
	}
	if (isApi(url)) return
	if (event.request.destination === "image" && cachesProjectCover(url)) {
		event.respondWith(cacheCover(event.request))
		return
	}
	if (immutableSet.has(url.pathname) || staticSet.has(url.pathname)) {
		event.respondWith(cacheFirst(event.request))
		return
	}
	if (event.request.mode === "navigate") {
		event.respondWith(networkFirst(event.request))
		return
	}
	event.respondWith(cacheFirst(event.request))
})
