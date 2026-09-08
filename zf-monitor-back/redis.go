package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisOperationTimeout = 300 * time.Millisecond
	redisHostsTTL         = 10 * time.Second
	redisSummaryTTL       = 5 * time.Second
	redisHostStateTTL     = 60 * time.Second
)

var redisClient *redis.Client

type redisHostLatest struct {
	HostID   string  `json:"hostId"`
	Hostname string  `json:"hostname"`
	LastSeen string  `json:"lastSeen"`
	CPU      float64 `json:"cpu"`
	Memory   float64 `json:"memory"`
	Disk     float64 `json:"disk"`
	NetUp    float64 `json:"netUp"`
	NetDown  float64 `json:"netDown"`
}

func initRedis() {
	address := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if address == "" {
		log.Printf("redis disabled: REDIS_ADDR is not configured")
		return
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:                  address,
		DialTimeout:           3 * time.Second,
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
		PoolTimeout:           3 * time.Second,
		ContextTimeoutEnabled: true,
	})
	if err := redisPing(); err != nil {
		log.Printf("redis unavailable addr=%s err=%v", address, err)
		return
	}
	log.Printf("redis connected addr=%s", address)
}

func closeRedis() {
	if redisClient != nil {
		if err := redisClient.Close(); err != nil {
			log.Printf("redis close failed: %v", err)
		}
	}
}

func redisContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisOperationTimeout)
}

func redisPing() error {
	if redisClient == nil {
		return fmt.Errorf("redis disabled")
	}
	ctx, cancel := redisContext()
	defer cancel()
	return redisClient.Ping(ctx).Err()
}

func redisGetJSON(ctx context.Context, key string, target interface{}) (bool, error) {
	if redisClient == nil {
		return false, nil
	}
	value, err := redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return false, err
	}
	return true, nil
}

func redisSetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if redisClient == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return redisClient.Set(ctx, key, encoded, ttl).Err()
}

func hostsCacheKey() string { return "stark:hosts:list" }

func summaryCacheKey(hostID string) string { return "stark:summary:" + hostID }

func hostLatestCacheKey(hostID string) string { return "stark:host:" + hostID + ":latest" }

func hostLastSeenCacheKey(hostID string) string { return "stark:host:" + hostID + ":last_seen" }

func getCachedHosts() ([]HostListItem, bool, error) {
	var hosts []HostListItem
	ctx, cancel := redisContext()
	defer cancel()
	hit, err := redisGetJSON(ctx, hostsCacheKey(), &hosts)
	return hosts, hit, err
}

func cacheHosts(hosts []HostListItem) error {
	ctx, cancel := redisContext()
	defer cancel()
	return redisSetJSON(ctx, hostsCacheKey(), hosts, redisHostsTTL)
}

func getCachedSummary(hostID string) (summaryResponse, bool, error) {
	var summary summaryResponse
	ctx, cancel := redisContext()
	defer cancel()
	hit, err := redisGetJSON(ctx, summaryCacheKey(hostID), &summary)
	return summary, hit, err
}

func cacheSummary(hostID string, summary summaryResponse) error {
	ctx, cancel := redisContext()
	defer cancel()
	return redisSetJSON(ctx, summaryCacheKey(hostID), summary, redisSummaryTTL)
}

func updateRedisAfterReport(report Report, lastSeen time.Time) {
	if redisClient == nil {
		return
	}

	state := redisHostLatest{
		HostID: report.HostID, Hostname: report.Hostname, LastSeen: lastSeen.UTC().Format(time.RFC3339),
		CPU: report.CPU, Memory: report.Memory, Disk: report.Disk, NetUp: report.NetUp, NetDown: report.NetDown,
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		log.Printf("redis report state encode failed hostId=%s err=%v", report.HostID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisOperationTimeout)
	defer cancel()
	_, err = redisClient.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, hostLatestCacheKey(report.HostID), encoded, redisHostStateTTL)
		pipe.Set(ctx, hostLastSeenCacheKey(report.HostID), state.LastSeen, redisHostStateTTL)
		pipe.Del(ctx, hostsCacheKey(), summaryCacheKey(report.HostID))
		return nil
	})
	if err != nil {
		log.Printf("redis report pipeline failed hostId=%s err=%v", report.HostID, err)
	}
}

func handleRedisStatus(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	address := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisClient == nil || address == "" {
		writeJSON(w, map[string]interface{}{"enabled": false, "status": "disabled"})
		return
	}

	started := time.Now()
	if err := redisPing(); err != nil {
		writeJSON(w, map[string]interface{}{"enabled": true, "status": "unavailable", "address": address})
		return
	}
	ctx, cancel := redisContext()
	defer cancel()
	dbSize, err := redisClient.DBSize(ctx).Result()
	if err != nil {
		writeJSON(w, map[string]interface{}{"enabled": true, "status": "unavailable", "address": address})
		return
	}
	writeJSON(w, map[string]interface{}{"enabled": true, "status": "healthy", "address": address, "latencyMs": time.Since(started).Milliseconds(), "dbSize": dbSize})
}

func handleRedisHostLatest(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostID := strings.TrimSpace(r.URL.Query().Get("hostId"))
	if hostID == "" {
		http.Error(w, "hostId is required", http.StatusBadRequest)
		return
	}
	if redisClient == nil {
		http.Error(w, "redis disabled", http.StatusServiceUnavailable)
		return
	}
	var state redisHostLatest
	ctx, cancel := redisContext()
	defer cancel()
	hit, err := redisGetJSON(ctx, hostLatestCacheKey(hostID), &state)
	if err != nil {
		http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
		return
	}
	if !hit {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, state)
}
