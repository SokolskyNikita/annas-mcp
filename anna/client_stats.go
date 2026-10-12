package anna

import (
	"container/list"
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

const (
	maxConcurrentFileStats = 4
	fileStatsBudget        = 5 * time.Second
	fileStatsCacheTTL      = time.Hour
	fileStatsCacheCapacity = 1024
)

// FileStats fetches public counters for valid MD5 hashes. Individual lookup
// failures are omitted from the result; cancellation or the caller's deadline
// is returned as an error. The five-second budget applies to the enrichment
// as a whole and is independent of the caller's longer operation timeout.
func (c *Client) FileStats(ctx context.Context, hashes []string) (map[string]RecordStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	results := make(map[string]RecordStats)
	if err := ctx.Err(); err != nil {
		return results, err
	}

	seen := make(map[string]struct{}, len(hashes))
	pending := make([]string, 0, len(hashes))
	for _, rawHash := range hashes {
		hash, err := normalizeHash(rawHash)
		if err != nil {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		if stats, ok := c.fileStatsCache.get(hash); ok {
			results[hash] = stats
			continue
		}
		pending = append(pending, hash)
	}
	if len(pending) == 0 {
		return results, ctx.Err()
	}

	budget := c.fileStatsTimeout
	if budget <= 0 {
		budget = fileStatsBudget
	}
	requestCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	workerCount := min(maxConcurrentFileStats, len(pending))
	jobs := make(chan string)
	var workers sync.WaitGroup
	var resultMu sync.Mutex
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for hash := range jobs {
				stats, err := c.fetchFileStats(requestCtx, hash)
				if err != nil {
					if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						c.log.Warn("Could not fetch public file statistics", "hash", hash, "error", err)
					}
					continue
				}
				resultMu.Lock()
				results[hash] = stats
				resultMu.Unlock()
			}
		}()
	}

dispatch:
	for _, hash := range pending {
		select {
		case jobs <- hash:
		case <-requestCtx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return results, err
	}
	return results, nil
}

func (c *Client) fetchFileStats(ctx context.Context, hash string) (RecordStats, error) {
	slots := c.fileStatsSemaphore()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return RecordStats{}, ctx.Err()
	}
	defer func() { <-slots }()

	// Another concurrent call may have populated the cache while this worker
	// waited for a slot.
	if stats, ok := c.fileStatsCache.get(hash); ok {
		return stats, nil
	}

	var stats RecordStats
	err := c.onMirror(ctx, func(base string) error {
		var response parse.MD5InlineInfo
		rawURL := buildPathURL(base, "/dyn/md5/inline_info/"+hash)
		header := make(http.Header)
		header.Set("Accept", "text/css")
		if err := c.fetchJSONWithHeaders(ctx, rawURL, &response, "public file statistics", header); err != nil {
			return err
		}
		stats = RecordStats{
			Downloads: response.DownloadsTotal, GreatQuality: response.GreatQualityCount,
			Reports: response.ReportsCount, Lists: response.ListsCount, Comments: response.CommentsCount,
		}
		return nil
	})
	if err != nil {
		return RecordStats{}, err
	}
	c.fileStatsCache.put(hash, stats)
	return stats, nil
}

func (c *Client) fileStatsSemaphore() chan struct{} {
	c.fileStatsInitMu.Lock()
	defer c.fileStatsInitMu.Unlock()
	if c.fileStatsSlots == nil {
		c.fileStatsSlots = make(chan struct{}, maxConcurrentFileStats)
	}
	return c.fileStatsSlots
}

type statsCacheEntry struct {
	hash      string
	stats     RecordStats
	expiresAt time.Time
}

// statsCache is an LRU with expiration. Values are copied on read and write,
// and contain only public counters.
type statsCache struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	order      *list.List
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

func newStatsCache() statsCache {
	return statsCache{
		entries: make(map[string]*list.Element), order: list.New(),
		ttl: fileStatsCacheTTL, maxEntries: fileStatsCacheCapacity, now: time.Now,
	}
}

func (cache *statsCache) get(hash string) (RecordStats, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.initialize()
	element, ok := cache.entries[hash]
	if !ok {
		return RecordStats{}, false
	}
	entry := element.Value.(*statsCacheEntry)
	if !cache.now().Before(entry.expiresAt) {
		cache.remove(element)
		return RecordStats{}, false
	}
	cache.order.MoveToFront(element)
	return entry.stats, true
}

func (cache *statsCache) put(hash string, stats RecordStats) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.initialize()
	now := cache.now()
	if element, ok := cache.entries[hash]; ok {
		entry := element.Value.(*statsCacheEntry)
		entry.stats = stats
		entry.expiresAt = now.Add(cache.ttl)
		cache.order.MoveToFront(element)
		return
	}
	if cache.maxEntries <= 0 {
		return
	}
	entry := &statsCacheEntry{hash: hash, stats: stats, expiresAt: now.Add(cache.ttl)}
	cache.entries[hash] = cache.order.PushFront(entry)
	for cache.order.Len() > cache.maxEntries {
		cache.remove(cache.order.Back())
	}
}

func (cache *statsCache) initialize() {
	if cache.entries == nil {
		cache.entries = make(map[string]*list.Element)
	}
	if cache.order == nil {
		cache.order = list.New()
	}
	if cache.ttl <= 0 {
		cache.ttl = fileStatsCacheTTL
	}
	if cache.maxEntries == 0 {
		cache.maxEntries = fileStatsCacheCapacity
	}
	if cache.now == nil {
		cache.now = time.Now
	}
}

func (cache *statsCache) remove(element *list.Element) {
	entry := element.Value.(*statsCacheEntry)
	delete(cache.entries, entry.hash)
	cache.order.Remove(element)
}
