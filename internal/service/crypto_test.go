package service

import (
	"testing"
	"time"
)

func TestSignVerifyRoundtrip(t *testing.T) {
	secret := "test-secret"
	code := "MACHINE-1234"
	ts := time.Now().Unix()

	sig := Sign(secret, code, ts)
	if err := Verify(secret, code, ts, sig, 60); err != nil {
		t.Fatalf("valid sig should verify: %v", err)
	}
}

func TestVerifyRejectsStaleTimestamp(t *testing.T) {
	secret := "s"
	code := "M"
	oldTs := time.Now().Unix() - 1000
	sig := Sign(secret, code, oldTs)
	if err := Verify(secret, code, oldTs, sig, 60); err == nil {
		t.Fatal("stale timestamp should be rejected")
	}
}

func TestVerifyRejectsTamperedSig(t *testing.T) {
	secret := "s"
	code := "M"
	ts := time.Now().Unix()
	sig := Sign(secret, code, ts)
	tampered := sig[:len(sig)-1] + "0"
	if tampered == sig {
		tampered = sig[:len(sig)-1] + "1"
	}
	if err := Verify(secret, code, ts, tampered, 60); err == nil {
		t.Fatal("tampered signature should be rejected")
	}
}
