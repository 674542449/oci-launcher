package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

const accountLockKey = "oci:global_account_lock"

func InitRedis(addr, password string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:           addr,
		Password:       password,
		DB:             0,
		PoolSize:       50,
		MinIdleConns:   10,
		MaxActiveConns: 200, // Pub/Sub sockets are not pooled; cap them so Redis maxclients is never exhausted
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	RDB = client
	log.Println("Redis connected successfully")
	return client, nil
}

// acquireOrRefreshScript takes the global lock when it is free, and refreshes the TTL when it is
// already held by the same profile. Returns 1 on success, 0 when another profile holds it.
var acquireOrRefreshScript = redis.NewScript(`
local cur = redis.call("GET", KEYS[1])
if not cur or cur == ARGV[1] then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return 1
end
return 0
`)

// releaseScript deletes the lock only if it is held by the given profile (compare-and-delete).
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// AcquireAccountLock enforces the "one OCI account operated at a time" rule.
// It succeeds when the lock is free OR already held by the same profile (the TTL is refreshed in
// that case), so a worker can renew its own lock every iteration. When another profile holds the
// lock, ok=false and lockedBy carries that profile id.
func AcquireAccountLock(ctx context.Context, profileID uint, ttl time.Duration) (ok bool, lockedBy string, err error) {
	val := fmt.Sprintf("%d", profileID)
	res, err := acquireOrRefreshScript.Run(ctx, RDB, []string{accountLockKey}, val, ttl.Milliseconds()).Int()
	if err != nil {
		return false, "", err
	}
	if res == 1 {
		return true, val, nil
	}
	cur, _ := RDB.Get(ctx, accountLockKey).Result()
	return false, cur, nil
}

// ReleaseAccountLock releases the global lock if this profile holds it.
func ReleaseAccountLock(ctx context.Context, profileID uint) error {
	val := fmt.Sprintf("%d", profileID)
	return releaseScript.Run(ctx, RDB, []string{accountLockKey}, val).Err()
}

// IsIPBlacklisted checks if an IP is banned
func IsIPBlacklisted(ctx context.Context, ip string) bool {
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return false
	}
	key := fmt.Sprintf("blacklist:ip:%s", ip)
	exists, err := RDB.Exists(ctx, key).Result()
	if err != nil {
		return false
	}
	return exists > 0
}

// BlacklistIP bans an IP for a specific duration
func BlacklistIP(ctx context.Context, ip string, duration time.Duration, reason string) error {
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return nil // Immune from blacklisting
	}
	key := fmt.Sprintf("blacklist:ip:%s", ip)
	return RDB.Set(ctx, key, reason, duration).Err()
}

// RecordLoginFailure records a login failure and returns lockout status and attempt count
func RecordLoginFailure(ctx context.Context, ip, username string) (isLocked bool, lockDuration time.Duration, attempts int64, err error) {
	key := fmt.Sprintf("auth:fail:%s", ip)
	pipe := RDB.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 30*time.Minute)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return false, 0, 0, err
	}

	count := incr.Val()
	if count >= 10 {
		_ = BlacklistIP(ctx, ip, 24*time.Hour, "Brute force login attack (10+ failures)")
		return true, 24 * time.Hour, count, nil
	} else if count >= 5 {
		_ = BlacklistIP(ctx, ip, 1*time.Hour, "Multiple login failures (5+ failures)")
		return true, 1 * time.Hour, count, nil
	} else if count >= 2 {
		// Progressive penalty delay
		time.Sleep(2 * time.Second)
	}

	return false, 0, count, nil
}

// ResetLoginFailures clears failure count after successful login
func ResetLoginFailures(ctx context.Context, ip string) {
	key := fmt.Sprintf("auth:fail:%s", ip)
	RDB.Del(ctx, key)
}

// RecordTOTPFailure counts wrong one-time codes per temp-token id. Returns the failure count.
func RecordTOTPFailure(ctx context.Context, tokenJTI string) (int64, error) {
	key := fmt.Sprintf("totp:fail:%s", tokenJTI)
	pipe := RDB.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 10*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// CheckAndSetTOTPUsed prevents TOTP replay attacks
func CheckAndSetTOTPUsed(ctx context.Context, userID uint, code string) bool {
	key := fmt.Sprintf("totp:used:%d:%s", userID, code)
	ok, err := RDB.SetNX(ctx, key, "1", 90*time.Second).Result()
	if err != nil || !ok {
		return false // already used or redis error
	}
	return true
}

// BlacklistJTI invalidates a JWT Token
func BlacklistJTI(ctx context.Context, jti string, ttl time.Duration) error {
	key := fmt.Sprintf("jwt:blacklist:%s", jti)
	return RDB.Set(ctx, key, "1", ttl).Err()
}

// IsJTIBlacklisted checks if token was revoked
func IsJTIBlacklisted(ctx context.Context, jti string) bool {
	key := fmt.Sprintf("jwt:blacklist:%s", jti)
	exists, err := RDB.Exists(ctx, key).Result()
	if err != nil {
		return false
	}
	return exists > 0
}

// PublishTaskLog publishes real-time log to Redis Pub/Sub
func PublishTaskLog(ctx context.Context, taskID string, logPayload string) error {
	channel := fmt.Sprintf("task:logs:%s", taskID)
	return RDB.Publish(ctx, channel, logPayload).Err()
}

// CacheMetadata stores metadata (Regions, ADs, Images) with TTL
func CacheMetadata(ctx context.Context, key string, data string, ttl time.Duration) error {
	cacheKey := fmt.Sprintf("meta:%s", key)
	return RDB.Set(ctx, cacheKey, data, ttl).Err()
}

// GetCachedMetadata retrieves cached metadata
func GetCachedMetadata(ctx context.Context, key string) (string, error) {
	cacheKey := fmt.Sprintf("meta:%s", key)
	return RDB.Get(ctx, cacheKey).Result()
}
