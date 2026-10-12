package anna

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileStatsNormalizesDeduplicatesAndCaches(t *testing.T) {
	t.Parallel()

	const hash = "0123456789abcdef0123456789abcdef"
	var calls atomic.Int32
	client := mustClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Path != "/dyn/md5/inline_info/"+hash {
			t.Errorf("request path = %s", req.URL.Path)
		}
		if got := req.Header.Get("Accept"); got != "text/css" {
			t.Errorf("Accept = %q", got)
		}
		if req.URL.RawQuery != "" {
			t.Errorf("unexpected query: %s", req.URL.RawQuery)
		}
		return fixtureResponse(t, req, http.StatusOK, "md5_inline_info.json", "text/css", nil), nil
	})}})

	got, err := client.FileStats(context.Background(), []string{" " + strings.ToUpper(hash) + " ", hash, "invalid", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := RecordStats{Downloads: 345, GreatQuality: 1, Reports: 2, Lists: 4}
	if len(got) != 1 || got[hash] != want {
		t.Fatalf("stats = %+v, want %s: %+v", got, hash, want)
	}
	if cached, err := client.FileStats(context.Background(), []string{hash}); err != nil || cached[hash] != want {
		t.Fatalf("cached stats = %+v, %v", cached, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("made %d requests; want one", calls.Load())
	}
}

func TestFileStatsKeepsSuccessfulLookupsWhenOneFails(t *testing.T) {
	t.Parallel()

	const (
		goodHash = "0123456789abcdef0123456789abcdef"
		badHash  = "abcdef0123456789abcdef0123456789"
	)
	var badCalls atomic.Int32
	client := mustClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, badHash) {
			if badCalls.Add(1) == 1 {
				return fixtureResponse(t, req, http.StatusServiceUnavailable, "upstream_error.html", "text/html", nil), nil
			}
		}
		return fixtureResponse(t, req, http.StatusOK, "md5_inline_info.json", "text/css", nil), nil
	})}})

	got, err := client.FileStats(context.Background(), []string{goodHash, badHash})
	if err != nil {
		t.Fatalf("best-effort lookup returned error: %v", err)
	}
	if len(got) != 1 || got[goodHash].Downloads != 345 {
		t.Fatalf("partial stats = %+v", got)
	}
	got, err = client.FileStats(context.Background(), []string{badHash})
	if err != nil || got[badHash].Downloads != 345 {
		t.Fatalf("retry after individual failure = %+v, %v", got, err)
	}
	if _, err := client.FileStats(context.Background(), []string{badHash}); err != nil || badCalls.Load() != 2 {
		t.Fatalf("successful retry was not cached: calls=%d, err=%v", badCalls.Load(), err)
	}
}

func TestFileStatsLimitsConcurrentRequestsAcrossCalls(t *testing.T) {
	t.Parallel()

	const calls = 3
	const hashesPerCall = 4
	entered := make(chan struct{}, calls*hashesPerCall)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	client := mustClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for seen := maximum.Load(); current > seen && !maximum.CompareAndSwap(seen, current); seen = maximum.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return fixtureResponse(t, req, http.StatusOK, "md5_inline_info.json", "text/css", nil), nil
	})}})

	var wait sync.WaitGroup
	wait.Add(calls)
	for call := range calls {
		go func(call int) {
			defer wait.Done()
			hashes := make([]string, hashesPerCall)
			for index := range hashesPerCall {
				hashes[index] = fmt.Sprintf("%032x", call*hashesPerCall+index+1)
			}
			if _, err := client.FileStats(context.Background(), hashes); err != nil {
				t.Errorf("FileStats: %v", err)
			}
		}(call)
	}

	for range maxConcurrentFileStats {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("fewer than four requests became active")
		}
	}
	select {
	case <-entered:
		close(release)
		t.Fatal("more than four statistics requests ran concurrently")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	wait.Wait()
	if maximum.Load() != maxConcurrentFileStats {
		t.Fatalf("maximum in-flight requests = %d", maximum.Load())
	}
}

func TestFileStatsBudgetIsBestEffortAndCallerCancellationIsReturned(t *testing.T) {
	t.Parallel()

	const hash = "0123456789abcdef0123456789abcdef"
	started := make(chan struct{}, 1)
	client := mustClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}})
	client.fileStatsTimeout = 20 * time.Millisecond

	if stats, err := client.FileStats(context.Background(), []string{hash}); err != nil || len(stats) != 0 {
		t.Fatalf("internal budget should be best effort: %+v, %v", stats, err)
	}
	<-started

	callerCtx, cancel := context.WithCancel(context.Background())
	client.fileStatsTimeout = time.Second
	result := make(chan error, 1)
	go func() {
		_, err := client.FileStats(callerCtx, []string{hash + " "})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("statistics request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("FileStats did not return after caller cancellation")
	}

	deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelDeadline()
	if _, err := client.FileStats(deadlineCtx, []string{hash}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline error = %v", err)
	}
	<-started
}

func TestStatsCacheExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	cache := newStatsCache()
	now := time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	for index := range fileStatsCacheCapacity {
		cache.put(fmt.Sprintf("%032x", index), RecordStats{Downloads: index})
	}
	firstHash := fmt.Sprintf("%032x", 0)
	secondHash := fmt.Sprintf("%032x", 1)
	newHash := fmt.Sprintf("%032x", fileStatsCacheCapacity)
	if got, ok := cache.get(firstHash); !ok || got.Downloads != 0 {
		t.Fatalf("first cache entry = %+v, %v", got, ok)
	}
	cache.put(newHash, RecordStats{Downloads: fileStatsCacheCapacity})
	if _, ok := cache.get(secondHash); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	if got, ok := cache.get(newHash); !ok || got.Downloads != fileStatsCacheCapacity {
		t.Fatalf("new cache entry = %+v, %v", got, ok)
	}
	now = now.Add(fileStatsCacheTTL)
	if _, ok := cache.get(firstHash); ok {
		t.Fatal("expired cache entry was returned")
	}
}
