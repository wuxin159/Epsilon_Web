package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrTimestampOutOfWindow = errors.New("timestamp out of window")
	ErrBadSignature         = errors.New("bad signature")
)

// Sign 返回 hex(HMAC-SHA256(secret, machineCode + "|" + timestamp))
// 客户端和服务端使用相同算法。
func Sign(secret, machineCode string, timestamp int64) string {
	h := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(h, "%s|%d", machineCode, timestamp)
	return hex.EncodeToString(h.Sum(nil))
}

// Verify 校验签名有效性 + 时间戳新鲜度 (防重放)。
// window 为允许的时间戳偏移秒数 (两侧)。
func Verify(secret, machineCode string, timestamp int64, sign string, window int64) error {
	now := time.Now().Unix()
	if timestamp < now-window || timestamp > now+window {
		return ErrTimestampOutOfWindow
	}
	expected := Sign(secret, machineCode, timestamp)
	if !hmac.Equal([]byte(expected), []byte(sign)) {
		return ErrBadSignature
	}
	return nil
}
