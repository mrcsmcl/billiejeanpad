package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

var (
	errStale  = errors.New("stale")
	errFuture = errors.New("timestamp from future")
	errBadSig = errors.New("bad signature")
)

// canonicalPayload builds the exact string that both the Go verifier and
// the browser signer agree on. Pipe-delimited to avoid JSON field-order
// ambiguity. Missing fields become empty strings.
func canonicalPayload(c cmd) string {
	random := "0"
	if c.Random {
		random = "1"
	}
	enabled := ""
	if c.Enabled != nil {
		if *c.Enabled {
			enabled = "1"
		} else {
			enabled = "0"
		}
	}
	return c.Cmd + "|" + c.Sound + "|" + c.Category + "|" +
		random + "|" + enabled + "|" + c.Preset + "|" +
		strconv.FormatInt(c.Ts, 10)
}

// sign returns the hex HMAC-SHA256 of the canonical payload under GroupKey.
func sign(c cmd) string {
	mac := hmac.New(sha256.New, []byte(GroupKey))
	mac.Write([]byte(canonicalPayload(c)))
	return hex.EncodeToString(mac.Sum(nil))
}

// verify checks the staleness window and the HMAC signature. Returns a
// typed error on any failure so the caller can log the specific reason.
func verify(c cmd) error {
	age := time.Now().Unix() - c.Ts
	if age > int64(MaxCmdAge) {
		return errStale
	}
	if age < -5 { // tolerate up to 5s of clock skew between phone and PC
		return errFuture
	}
	expected := sign(c)
	// hmac.Equal is constant-time, which protects against trivial timing
	// side channels even though this isn't a high-value target.
	if !hmac.Equal([]byte(c.Sig), []byte(expected)) {
		return errBadSig
	}
	return nil
}

// canonicalHeartbeat is mirrored by the PWA. Keep field order stable.
func canonicalHeartbeat(h heartbeat) string {
	timerEnabled := "0"
	if h.Timer {
		timerEnabled = "1"
	}
	return h.Host + "|" + h.Version + "|" + strconv.FormatInt(h.Ts, 10) +
		"|" + timerEnabled + "|" + h.Preset
}

func signHeartbeat(h heartbeat) string {
	mac := hmac.New(sha256.New, []byte(GroupKey))
	mac.Write([]byte(canonicalHeartbeat(h)))
	return hex.EncodeToString(mac.Sum(nil))
}
