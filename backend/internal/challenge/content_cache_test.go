package challenge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

func cacheEntry(id uuid.UUID, hash string, bytes int64) *versionCacheEntry {
	return &versionCacheEntry{
		key: versionCacheKey{id: id, hash: hash}, bytes: bytes,
		document: &VersionDocument{Version: storage.ChallengeVersion{ID: id, ContentHash: hash}},
	}
}

func TestVersionContentCacheEvictsByCountAndBytes(t *testing.T) {
	cache := newVersionContentCache(CacheConfig{MaxVersions: 2, MaxBytes: 10})
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	cache.put(cacheEntry(first, "a", 4))
	cache.put(cacheEntry(second, "b", 4))
	cache.put(cacheEntry(third, "c", 4))
	if _, ok := cache.get(first); ok {
		t.Fatal("oldest entry was not evicted by count")
	}
	if stats := cache.stats(); stats.Entries != 2 || stats.EstimatedBytes != 8 || stats.Evictions != 1 {
		t.Fatalf("unexpected count eviction stats: %+v", stats)
	}

	cache = newVersionContentCache(CacheConfig{MaxVersions: 4, MaxBytes: 7})
	cache.put(cacheEntry(first, "a", 4))
	cache.put(cacheEntry(second, "b", 4))
	if _, ok := cache.get(first); ok {
		t.Fatal("oldest entry was not evicted by byte limit")
	}
	if stats := cache.stats(); stats.Entries != 1 || stats.EstimatedBytes != 4 {
		t.Fatalf("unexpected byte eviction stats: %+v", stats)
	}
}

func TestVersionContentCacheReplacesHashForSameVersion(t *testing.T) {
	cache := newVersionContentCache(CacheConfig{MaxVersions: 2, MaxBytes: 100})
	id := uuid.New()
	cache.put(cacheEntry(id, "old", 10))
	cache.put(cacheEntry(id, "new", 20))
	entry, ok := cache.get(id)
	if !ok || entry.key.hash != "new" {
		t.Fatalf("cache returned stale hash: %#v, %v", entry, ok)
	}
	if stats := cache.stats(); stats.Entries != 1 || stats.EstimatedBytes != 20 {
		t.Fatalf("replacement accounting is incorrect: %+v", stats)
	}
}

func TestVersionContentCacheDoesNotRetainOversizedEntry(t *testing.T) {
	cache := newVersionContentCache(CacheConfig{MaxVersions: 2, MaxBytes: 10})
	cache.put(cacheEntry(uuid.New(), "large", 11))
	if stats := cache.stats(); stats.Entries != 0 || stats.EstimatedBytes != 0 {
		t.Fatalf("oversized entry was retained: %+v", stats)
	}
}

func TestVersionContentCacheDoesNotRetainDraftWithoutHash(t *testing.T) {
	cache := newVersionContentCache()
	cache.put(cacheEntry(uuid.New(), "", 10))
	if stats := cache.stats(); stats.Entries != 0 {
		t.Fatalf("draft-like entry was retained: %+v", stats)
	}
}

func TestVersionContentCacheCoalescesColdLoad(t *testing.T) {
	cache := newVersionContentCache()
	id := uuid.New()
	_, flight, leader := cache.acquire(id)
	if !leader {
		t.Fatal("first loader was not elected leader")
	}
	_, waiting, secondLeader := cache.acquire(id)
	if secondLeader || waiting != flight {
		t.Fatal("second loader did not join the existing flight")
	}
	entry := cacheEntry(id, "hash", 10)
	var (
		got *versionCacheEntry
		err error
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		got, err = cache.wait(context.Background(), waiting)
	}()
	cache.finishLoad(id, flight, entry, nil, time.Millisecond, time.Millisecond)
	wg.Wait()
	if err != nil || got != entry {
		t.Fatalf("coalesced waiter result = %#v, %v", got, err)
	}
	if stats := cache.stats(); stats.Loads != 1 || stats.CoalescedLoads != 1 {
		t.Fatalf("unexpected load stats: %+v", stats)
	}
}

func TestCompiledVersionContentPrecomputesIndexes(t *testing.T) {
	doc := validVersionDocument(t)
	doc.Version.ID = uuid.New()
	doc.Version.Status = storage.ChallengeVersionPublished
	var manifest Manifest
	if err := json.Unmarshal(doc.Version.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.DefaultConfig.RequiredWorldBooks = []string{"rules"}
	doc.Version.Manifest, _ = json.Marshal(manifest)
	doc.Assets = append(doc.Assets, storage.ChallengeAsset{
		Kind: storage.ChallengeAssetWorldBook, Slug: "rules", Name: "Rules",
		Content: storage.JSONDoc(`{"id":"rules","entries":[{"id":"entry","title":"Rule","content":"Some rule text","keywords":["  FATE ","fate"],"enabled":true,"metadata":{"inquiry_visible":true}}]}`),
	})
	for index := range doc.Assets {
		doc.Assets[index].ID = uuid.New()
		doc.Assets[index].ChallengeVersionID = doc.Version.ID
	}
	hash, err := ContentHashDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Version.ContentHash = hash
	compiled, size, err := compileVersionContent(doc)
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 || compiled.prompt.ID != "prompt" || len(compiled.assetsByID) != len(doc.Assets) {
		t.Fatalf("compiled content is incomplete: size=%d assets=%d", size, len(compiled.assetsByID))
	}
	if got := compiled.public.personas; len(got) != 1 || got[0].Slug != "persona" {
		t.Fatalf("public persona projection = %#v", got)
	}
	if len(compiled.worldEntries) != 1 || compiled.worldEntries[0].TokenCount == nil || len(compiled.worldEntries[0].Keywords) != 1 || compiled.worldEntries[0].Keywords[0] != "fate" {
		t.Fatalf("compiled worldbook entry = %#v", compiled.worldEntries)
	}
	if len(compiled.inquiryRules) != 1 || compiled.inquiryRules[0].Title != "Rule" {
		t.Fatalf("compiled inquiry rules = %#v", compiled.inquiryRules)
	}
}

func TestDefinitionsForVersionReturnsPrivateNestedMaps(t *testing.T) {
	source := map[string]storage.GameplayDefinition{
		"item:herb": {Kind: "item", Key: "herb", Tags: []string{"plant"}, Content: storage.JSONMap{"nested": map[string]any{"value": "original"}}},
	}
	clone := cloneGameplayDefinitions(source)
	row := clone["item:herb"]
	row.Tags[0] = "changed"
	row.Content["nested"].(map[string]any)["value"] = "changed"
	clone["item:herb"] = row
	original := source["item:herb"]
	if original.Tags[0] != "plant" || original.Content["nested"].(map[string]any)["value"] != "original" {
		t.Fatal("caller mutation leaked into cached definitions")
	}
}

func TestCloneVersionDocumentDoesNotShareMutableContent(t *testing.T) {
	source := validVersionDocument(t)
	clone, err := cloneVersionDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	clone.Assets[0].Content[0] = 'X'
	clone.Outcomes[0].RewardPoints = 999
	if source.Assets[0].Content[0] == 'X' || source.Outcomes[0].RewardPoints == 999 {
		t.Fatal("document clone shares mutable cache content")
	}
}

func BenchmarkCompileVersionContent(b *testing.B) {
	doc := validVersionDocument(b)
	doc.Version.ID = uuid.New()
	for index := range doc.Assets {
		doc.Assets[index].ID = uuid.New()
		doc.Assets[index].ChallengeVersionID = doc.Version.ID
	}
	hash, _ := ContentHashDocument(doc)
	doc.Version.ContentHash = hash
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := compileVersionContent(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVersionContentCacheHit(b *testing.B) {
	cache := newVersionContentCache()
	id := uuid.New()
	cache.put(cacheEntry(id, "hash", 1024))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, ok := cache.get(id); !ok {
			b.Fatal("cache miss")
		}
	}
}
