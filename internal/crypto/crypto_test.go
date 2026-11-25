// SPDX-License-Identifier: MIT
package crypto

import (
	"encoding/base32"
	"path/filepath"
	"testing"
	"time"
)

func TestPasswordHashAndVerify(t *testing.T) {
	pw := "Correct-Horse-Battery-Staple-99"
	if err := ValidatePasswordStrength(pw); err != nil {
		t.Fatalf("unexpected strength error: %v", err)
	}
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	if !VerifyPassword(pw, h) {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword("wrong-password-1234", h) {
		t.Fatal("expected wrong password to fail")
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	if err := ValidatePasswordStrength("short"); err == nil {
		t.Fatal("expected short password error")
	}
	if err := ValidatePasswordStrength("password1234"); err == nil {
		t.Fatal("expected common password error")
	}
}

func TestEnvelopeEncryption(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "master.key")
	enc, err := LoadOrCreateMasterKey(keyPath)
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	ct, err := enc.Encrypt("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	pt, err := enc.Decrypt(ct)
	if err != nil || pt != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("decrypt mismatch: %v %q", err, pt)
	}
}

func TestTOTP(t *testing.T) {
	sec, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("totp secret: %v", err)
	}
	now := time.Unix(1700000000, 0).UTC()
	// Generate expected code using internal hotp
	code := hotpMust(t, sec, uint64(now.Unix()/30))
	if !VerifyTOTP(sec, code, now) {
		t.Fatal("expected valid TOTP")
	}
	if VerifyTOTP(sec, "000000", now) && code != "000000" {
		t.Fatal("expected invalid TOTP to fail")
	}
}

func hotpMust(t *testing.T, sec string, counter uint64) string {
	t.Helper()
	u := OTPAuthURL("Stako", "admin", sec)
	if u == "" {
		t.Fatal("empty otpauth url")
	}
	// RFC 6238 test vector: secret = GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ ("12345678901234567890"), time=59 -> 287082
	if !VerifyTOTP("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "287082", time.Unix(59, 0).UTC()) {
		t.Fatal("RFC 6238 test vector failed")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(sec)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return hotp(key, counter)
}
