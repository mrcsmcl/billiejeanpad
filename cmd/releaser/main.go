package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type keyFile struct {
	PrivateKey string `json:"privateKey"`
}

type manifest struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	PublishedAt string `json:"publishedAt"`
	Signature   string `json:"signature"`
}

func main() {
	initOnly := flag.Bool("init", false, "create signing key if missing and print public key")
	version := flag.String("version", "", "release version")
	asset := flag.String("asset", "", "generic update executable")
	url := flag.String("url", "", "immutable release asset URL")
	out := flag.String("out", "docs/update.json", "manifest output path")
	keyPath := flag.String("key", "release-key.local.json", "private signing key path")
	flag.Parse()

	privateKey, err := loadOrCreateKey(*keyPath)
	fatalIf(err)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	fmt.Printf("release public key: %s\n", hex.EncodeToString(publicKey))
	if *initOnly {
		return
	}
	if *version == "" || *asset == "" || *url == "" {
		fatalIf(fmt.Errorf("version, asset, and url are required unless -init is used"))
	}

	data, err := os.ReadFile(*asset)
	fatalIf(err)
	info, err := os.Stat(*asset)
	fatalIf(err)
	digest := sha256.Sum256(data)
	item := manifest{
		Version:     *version,
		URL:         *url,
		SHA256:      hex.EncodeToString(digest[:]),
		Size:        info.Size(),
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}
	item.Signature = hex.EncodeToString(ed25519.Sign(privateKey, []byte(canonical(item))))

	payload, err := json.MarshalIndent(item, "", "  ")
	fatalIf(err)
	fatalIf(os.MkdirAll(filepath.Dir(*out), 0o755))
	fatalIf(os.WriteFile(*out, append(payload, '\n'), 0o644))
	fmt.Printf("manifest written: %s\n", *out)
}

func canonical(item manifest) string {
	return item.Version + "|" + item.URL + "|" + item.SHA256 + "|" +
		strconv.FormatInt(item.Size, 10) + "|" + item.PublishedAt
}

func loadOrCreateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var saved keyFile
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, err
		}
		raw, err := hex.DecodeString(saved.PrivateKey)
		if err != nil || len(raw) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid release private key")
		}
		return ed25519.PrivateKey(raw), nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	_ = publicKey
	payload, _ := json.MarshalIndent(keyFile{PrivateKey: hex.EncodeToString(privateKey)}, "", "  ")
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return nil, err
	}
	fmt.Printf("created private release key: %s\n", path)
	return privateKey, nil
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
