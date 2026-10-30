package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxUpdateSize = 32 << 20

type updateManifest struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	PublishedAt string `json:"publishedAt"`
	Signature   string `json:"signature"`
}

var updateMu sync.Mutex

func updateLoop(ctx context.Context) {
	// Leave startup and networking time to settle, then check twice a day.
	first := time.NewTimer(45*time.Second + time.Duration(secureRandomInt(60))*time.Second)
	defer first.Stop()
	select {
	case <-first.C:
		checkAndApplyUpdate(ctx)
	case <-ctx.Done():
		return
	}

	for {
		wait := 12*time.Hour + time.Duration(secureRandomInt(int64(30*time.Minute)))
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			checkAndApplyUpdate(ctx)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func checkAndApplyUpdate(ctx context.Context) {
	updateMu.Lock()
	defer updateMu.Unlock()

	installed, _ := sameFile(mustExecutablePath(), installExePath())
	if !installed {
		return
	}
	manifest, err := fetchUpdateManifest(ctx)
	if err != nil {
		logf("update check: %v", err)
		return
	}
	if compareVersions(manifest.Version, Version) <= 0 {
		logf("update check: current=%s latest=%s", Version, manifest.Version)
		return
	}
	if failedVersionRecently(manifest.Version) {
		logf("update %s skipped after recent rollback", manifest.Version)
		return
	}

	pending := filepath.Join(installDir(), "bjp-agent.update.exe")
	if err := downloadAndVerifyUpdate(ctx, manifest, pending); err != nil {
		logf("update %s download: %v", manifest.Version, err)
		_ = os.Remove(pending)
		return
	}
	logf("update %s verified; applying silently", manifest.Version)
	if err := applyUpdate(pending, manifest.Version); err != nil {
		logf("update %s apply: %v", manifest.Version, err)
		_ = os.Remove(pending)
	}
}

func fetchUpdateManifest(ctx context.Context) (updateManifest, error) {
	var item updateManifest
	requestURL := UpdateManifestURL + "?v=" + strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return item, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return item, errors.New("update manifest not published yet")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return item, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&item); err != nil {
		return item, err
	}
	if err := verifyUpdateManifest(item); err != nil {
		return item, err
	}
	return item, nil
}

func verifyUpdateManifest(item updateManifest) error {
	if compareVersions(item.Version, "0.0.0") <= 0 || item.Size <= 0 || item.Size > maxUpdateSize {
		return errors.New("invalid manifest values")
	}
	parsedURL, err := url.Parse(item.URL)
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Host, "github.com") {
		return errors.New("update URL is not an allowed GitHub HTTPS URL")
	}
	if !strings.HasPrefix(parsedURL.Path, "/mrcsmcl/billiejeanpad/releases/download/") {
		return errors.New("update URL is outside the Billie Jean Pad releases")
	}
	publicKey, err := hex.DecodeString(ReleasePublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid embedded release public key")
	}
	signature, err := hex.DecodeString(item.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid manifest signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(canonicalUpdateManifest(item)), signature) {
		return errors.New("manifest signature verification failed")
	}
	return nil
}

func canonicalUpdateManifest(item updateManifest) string {
	return item.Version + "|" + item.URL + "|" + item.SHA256 + "|" +
		strconv.FormatInt(item.Size, 10) + "|" + item.PublishedAt
}

func downloadAndVerifyUpdate(ctx context.Context, item updateManifest, destination string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", item.URL, nil)
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("asset HTTP %d", resp.StatusCode)
	}

	tmp := destination + ".download"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxUpdateSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != item.Size || written > maxUpdateSize {
		return fmt.Errorf("update size mismatch: got %d, expected %d", written, item.Size)
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actualHash, item.SHA256) {
		return errors.New("update SHA-256 mismatch")
	}
	_ = os.Remove(destination)
	return os.Rename(tmp, destination)
}

func applyUpdate(pending, newVersion string) error {
	cmd := exec.Command(
		pending,
		"--internal-apply-update",
		"--parent-pid", strconv.Itoa(os.Getpid()),
		"--target-exe", installExePath(),
		"--new-version", newVersion,
	)
	cmd.SysProcAttr = hiddenProcessAttributes()
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func markHealthyLater(ctx context.Context) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		_ = os.WriteFile(filepath.Join(installDir(), "healthy.version"), []byte(Version), 0o600)
	case <-ctx.Done():
	}
}

func failedVersionRecently(version string) bool {
	data, err := os.ReadFile(filepath.Join(installDir(), "update.failed"))
	if err != nil {
		return false
	}
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(parts) != 2 || parts[0] != version {
		return false
	}
	timestamp, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && time.Since(time.Unix(timestamp, 0)) < 24*time.Hour
}

func compareVersions(left, right string) int {
	parse := func(value string) [3]int {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		parts := strings.Split(value, ".")
		var result [3]int
		for i := 0; i < len(parts) && i < 3; i++ {
			clean := strings.SplitN(parts[i], "-", 2)[0]
			result[i], _ = strconv.Atoi(clean)
		}
		return result
	}
	a, b := parse(left), parse(right)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func mustExecutablePath() string {
	path, _ := os.Executable()
	return path
}
