package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	forgeCacheTTL       = 5 * time.Minute
	forgeCommitTTL      = 30 * time.Second
	forgeTreeTTL        = 2 * time.Minute
	forgeUnavailableTTL = 30 * time.Second
)

// forgeCacheEntry is one bounded GitHub response. A negative entry stores a failure for a short window so a broken forge is not hammered.
type forgeCacheEntry struct {
	body    []byte
	status  int
	etag    string
	err     error
	expires time.Time
}

// forgeCache keeps responses for one app instance. The key includes the request path and Accept header.
type forgeCache struct {
	mu      sync.Mutex
	entries map[string]forgeCacheEntry
}

// newForgeCache builds an empty cache.
func newForgeCache() *forgeCache {
	return &forgeCache{entries: map[string]forgeCacheEntry{}}
}

// lookup returns a fresh entry.
func (c *forgeCache) lookup(key string, now time.Time) (forgeCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok || now.After(entry.expires) {
		return forgeCacheEntry{}, false
	}

	return entry, true
}

// clear drops every entry, so an explicit refresh reads the forge again.
func (c *forgeCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = map[string]forgeCacheEntry{}
}

// store keeps an entry until its window ends.
func (c *forgeCache) store(key string, entry forgeCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = entry
}

// forgeTTL picks a shorter window for the resources that move most often.
func forgeTTL(path string) time.Duration {
	switch {
	case strings.Contains(path, "/commits"):
		return forgeCommitTTL
	case strings.Contains(path, "/git/trees/"):
		return forgeTreeTTL
	}

	return forgeCacheTTL
}

// cachedGet performs one bounded GitHub request through the cache, revalidating with ETag when the entry is stale.
func (c githubClient) cachedGet(ctx context.Context, path, accept string) ([]byte, int, error) {
	key := path + "\x00" + accept
	now := c.now()

	if entry, ok := c.cache.lookup(key, now); ok {
		return entry.body, entry.status, entry.err
	}

	if entry, ok := c.cache.stale(key); ok && entry.etag != "" {
		body, status, etag, err := c.fetch(ctx, path, accept, entry.etag)
		if status == http.StatusNotModified {
			entry.expires = now.Add(forgeTTL(path))
			c.cache.store(key, entry)

			return entry.body, entry.status, entry.err
		}

		c.cacheResult(key, path, body, status, etag, err, now)

		return body, status, err
	}

	body, status, etag, err := c.fetch(ctx, path, accept, "")
	c.cacheResult(key, path, body, status, etag, err, now)

	return body, status, err
}

// cacheResult stores a successful body or a short negative entry.
func (c githubClient) cacheResult(key, path string, body []byte, status int, etag string, err error, now time.Time) {
	switch {
	case err != nil || status >= http.StatusInternalServerError:
		c.cache.store(key, forgeCacheEntry{status: status, err: err, expires: now.Add(forgeUnavailableTTL)})
	case status == http.StatusOK:
		c.cache.store(key, forgeCacheEntry{body: body, status: status, etag: etag, expires: now.Add(forgeTTL(path))})
	}
}

// stale returns an expired entry that can still be revalidated.
func (c *forgeCache) stale(key string) (forgeCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]

	return entry, ok
}
