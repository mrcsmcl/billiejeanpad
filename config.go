package main

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	Version = "0.3.3"

	UpdateManifestURL = "https://mrcsmcl.github.io/billiejeanpad/update.json"
	ReleasePublicKey  = "f4565d8a24d7cc7d839d1e49d239464353cb93151e4e44c267dd8a5090ba3129"

	// BaseURL is the ntfy server. Point at a self-hosted server if the
	// public ntfy.sh rate limits become a problem.
	BaseURL = "https://ntfy.sh"

	// MaxCmdAge is the command staleness window in seconds.
	MaxCmdAge = 30

	// MinPlayGap is the local rate limit between sounds, in seconds.
	MinPlayGap = 3

	AppName          = "BillieJeanPad"
	InstalledExeName = "BJPAgent.exe"
	LegacyExeName    = "billiejeanpad.exe"
	MutexName        = `Global\BillieJeanPad`
)

// GroupKey and DeleteLauncher are injected by scripts/build.ps1 through
// linker -X variables. One group key acts as the HMAC key; the public ntfy
// topic namespace is derived from its SHA-256 hash, so operators only need
// one credential.
var (
	GroupKey       = "UNSET"
	DeleteLauncher = "false"
)

// topicPrefix derives a 128-bit, non-secret namespace from GroupKey. Knowing
// this prefix does not reveal the key required to sign commands.
func topicPrefix() string {
	sum := sha256.Sum256([]byte(GroupKey))
	return "bjp-" + hex.EncodeToString(sum[:16])
}
