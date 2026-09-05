package cache

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

const accountLockKey = "oci:global_account_lock"

// Lockout policy (per client IP, 30-minute window)
const (
	loginDelayAfter   = 3  // wrong passwords before each further attempt is slowed down
	loginBanAfter     = 6  // wrong passwords -> 30 min ban
	loginLongBanAfter = 12 // wrong passwords -> 24 h ban
	totpTokenMaxFails = 5  // wrong codes per temp token -> token consumed
	totpIPBanAfter    = 15 // wrong codes per IP -> 1 h ban
	failureWindow     = 30 * time.Minute
	loginBanDuration  = 30 * time.Minute
	longBanDuration   = 24 * time.Hour
	totpBanDuration   = 1 * time.Hour
)

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

// IsImmuneIP reports addresses that must never be banned: loopback, link-local and private
// ranges. Docker networks are private, so this covers the nginx / Cloudflare Tunnel containers —
// banning one of those would lock every visitor out at once.
func IsImmuneIP(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "localhost" {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified()
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

func blacklistKey(ip string) string { return fmt.Sprintf("blacklist:ip:%s", ip) }

// IsIPBlacklisted checks if an IP is banned
func IsIPBlacklisted(ctx context.Context, ip string) bool {
	if IsImmuneIP(ip) {
		return false
	}
	exists, err := RDB.Exists(ctx, blacklistKey(ip)).Result()
	if err != nil {
		return false
	}
	return exists > 0
}

// BlacklistIP bans an IP for a specific duration (immune addresses are only logged)
func BlacklistIP(ctx context.Context, ip string, duration time.Duration, reason string) error {
	if IsImmuneIP(ip) {
		log.Printf("[Security] not banning immune address %s (%s)", ip, reason)
		return nil
	}
	return RDB.Set(ctx, blacklistKey(ip), reason, duration).Err()
}

// BanInfo describes one active ban
type BanInfo struct {
	IP        string `json:"ip"`
	Reason    string `json:"reason"`
	ExpiresIn int64  `json:"expires_in_secs"`
}

// ListBannedIPs returns every active ban with its reason and remaining time
func ListBannedIPs(ctx context.Context) ([]BanInfo, error) {
	bans := []BanInfo{}
	iter := RDB.Scan(ctx, 0, "blacklist:ip:*", 200).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		ip := strings.TrimPrefix(key, "blacklist:ip:")
		reason, _ := RDB.Get(ctx, key).Result()
		ttl, _ := RDB.TTL(ctx, key).Result()
		bans = append(bans, BanInfo{IP: ip, Reason: reason, ExpiresIn: int64(ttl.Seconds())})
	}
	return bans, iter.Err()
}

// UnbanIP lifts the ban and clears the failure counters of an IP
func UnbanIP(ctx context.Context, ip string) error {
	return RDB.Del(ctx, blacklistKey(ip), fmt.Sprintf("auth:fail:%s", ip), fmt.Sprintf("totp:failip:%s", ip)).Err()
}

// RecordLoginFailure counts wrong passwords per IP and returns the lockout status.
// Policy: from the 3rd failure each attempt is slowed down, 6 failures -> 30 min ban, 12 -> 24 h.
func RecordLoginFailure(ctx context.Context, ip, username string) (isLocked bool, lockDuration time.Duration, attempts int64, err error) {
	key := fmt.Sprintf("auth:fail:%s", ip)
	pipe := RDB.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, failureWindow)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return false, 0, 0, err
	}

	count := incr.Val()
	switch {
	case count >= loginLongBanAfter:
		_ = BlacklistIP(ctx, ip, longBanDuration, fmt.Sprintf("Brute force login (%d failures)", count))
		return true, longBanDuration, count, nil
	case count >= loginBanAfter:
		_ = BlacklistIP(ctx, ip, loginBanDuration, fmt.Sprintf("Repeated login failures (%d)", count))
		return true, loginBanDuration, count, nil
	case count >= loginDelayAfter:
		time.Sleep(2 * time.Second)
	}
	return false, 0, count, nil
}

// ResetLoginFailures clears failure counters after a successful login
func ResetLoginFailures(ctx context.Context, ip string) {
	RDB.Del(ctx, fmt.Sprintf("auth:fail:%s", ip), fmt.Sprintf("totp:failip:%s", ip))
}

// RecordTOTPFailure counts wrong one-time codes per temp token and per IP.
// tokenExhausted: the temp token must be consumed (5 wrong codes); ipLocked: the IP was banned (15 in 30 min).
func RecordTOTPFailure(ctx context.Context, tokenJTI, ip string) (tokenFails, ipFails int64, tokenExhausted, ipLocked bool, err error) {
	tokenKey := fmt.Sprintf("totp:fail:%s", tokenJTI)
	ipKey := fmt.Sprintf("totp:failip:%s", ip)
	pipe := RDB.Pipeline()
	tokenIncr := pipe.Incr(ctx, tokenKey)
	pipe.Expire(ctx, tokenKey, 10*time.Minute)
	ipIncr := pipe.Incr(ctx, ipKey)
	pipe.Expire(ctx, ipKey, failureWindow)
	if _, err = pipe.Exec(ctx); err != nil {
		return 0, 0, false, false, err
	}
	tokenFails, ipFails = tokenIncr.Val(), ipIncr.Val()
	tokenExhausted = tokenFails >= totpTokenMaxFails
	if ipFails >= totpIPBanAfter {
		_ = BlacklistIP(ctx, ip, totpBanDuration, fmt.Sprintf("Repeated wrong 2FA codes (%d)", ipFails))
		ipLocked = true
	}
	if tokenFails >= 2 {
		time.Sleep(1 * time.Second)
	}
	return
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
