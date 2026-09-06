package auth

import (
	"os"
	"testing"
	"time"

	"oci-panel/internal/config"
	"oci-panel/internal/storage"
)

func TestMain(m *testing.M) {
	// getJWTSecret derives the signing key from the master key loaded at startup.
	config.GlobalConfig = &config.Config{MasterKey: "unit-test-master-key-0123456789"}
	os.Exit(m.Run())
}

func TestIssueSessionTokenLifetimes(t *testing.T) {
	user := &storage.User{ID: 7, Username: "admin", TokenVersion: 3}
	now := time.Now()

	// Fresh login: idle cap applies.
	token, _, exp, err := issueSessionToken(user, "Mozilla/5.0 test", "203.0.113.9", now)
	if err != nil {
		t.Fatalf("fresh session: %v", err)
	}
	if d := time.Until(exp); d < SessionTokenLifetime-time.Minute || d > SessionTokenLifetime+time.Minute {
		t.Fatalf("fresh session expiry %v, want about %v", d, SessionTokenLifetime)
	}
	claims, err := ValidateJWT(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.SessionStart != now.Unix() || !claims.Is2FAVerified || claims.TokenVersion != 3 {
		t.Fatalf("claims not carried over: %+v", claims)
	}
	if !sessionStartOf(claims).Equal(time.Unix(now.Unix(), 0)) {
		t.Fatalf("sessionStartOf = %v, want %v", sessionStartOf(claims), now)
	}

	// Renewal near the absolute cap: expiry is clamped to sessionStart + 30 d.
	start := now.Add(-sessionAbsoluteMax + 2*time.Hour)
	_, _, exp, err = issueSessionToken(user, "Mozilla/5.0 test", "", start)
	if err != nil {
		t.Fatalf("near cap: %v", err)
	}
	if d := time.Until(exp); d > 2*time.Hour+time.Minute || d < time.Hour {
		t.Fatalf("near-cap expiry %v, want about 2h", d)
	}

	// Past the absolute cap: no token.
	if _, _, _, err = issueSessionToken(user, "Mozilla/5.0 test", "", now.Add(-sessionAbsoluteMax)); err == nil {
		t.Fatal("expected an error once the session passed its absolute lifetime")
	}
}

func TestFingerprintIgnoresIP(t *testing.T) {
	user := &storage.User{ID: 1, Username: "admin", TokenVersion: 1}
	a, _, err := GenerateFullJWT(user, "UA-1", "198.51.100.1")
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := ValidateJWT(a)
	b, _, _ := GenerateFullJWT(user, "UA-1", "2001:db8::1")
	cb, _ := ValidateJWT(b)
	if ca.DeviceFingerprint != cb.DeviceFingerprint {
		t.Fatal("fingerprint must not depend on the client IP")
	}
	c, _, _ := GenerateFullJWT(user, "UA-2", "198.51.100.1")
	cc, _ := ValidateJWT(c)
	if ca.DeviceFingerprint == cc.DeviceFingerprint {
		t.Fatal("fingerprint must depend on the User-Agent")
	}
}
