package main

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisJSONCacheRoundTripAndTTL(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	previousClient := redisClient
	redisClient = redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() {
		_ = redisClient.Close()
		redisClient = previousClient
	}()

	value := summaryResponse{HostID: "test-host", Status: "online"}
	if err := cacheSummary(value.HostID, value); err != nil {
		t.Fatal(err)
	}
	if ttl := server.TTL(summaryCacheKey(value.HostID)); ttl <= 0 || ttl > redisSummaryTTL {
		t.Fatalf("unexpected summary TTL: %s", ttl)
	}

	cached, hit, err := getCachedSummary(value.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if !hit || cached.HostID != value.HostID || cached.Status != value.Status {
		t.Fatalf("unexpected cached value: hit=%v value=%+v", hit, cached)
	}
}

func TestRedisKeysUseStarkNamespace(t *testing.T) {
	keys := []string{
		hostsCacheKey(), summaryCacheKey("host"), hostLatestCacheKey("host"), hostLastSeenCacheKey("host"),
	}
	for _, key := range keys {
		if len(key) < len("stark:") || key[:len("stark:")] != "stark:" {
			t.Fatalf("key does not use stark namespace: %s", key)
		}
	}
	if redisHostStateTTL != 60*time.Second {
		t.Fatalf("unexpected runtime state TTL: %s", redisHostStateTTL)
	}
}
