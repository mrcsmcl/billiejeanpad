// Package sounds embeds the curated WAV catalog into the binary and
// exposes lookup helpers.
package sounds

import (
	"embed"
	"math/rand"
	"strings"
)

//go:embed subtle/*.wav punchy/*.wav scare/*.wav
var files embed.FS

// Sound is one playable clip. Name is the normalized id (lowercase,
// underscore separators); Data is the raw WAV bytes ready for PlaySound.
type Sound struct {
	Name     string
	Category string // "subtle", "punchy", or "scare"
	Data     []byte
}

// All holds every embedded clip, populated at init time.
var All []Sound

func init() {
	load := func(cat string) {
		entries, err := files.ReadDir(cat)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := files.ReadFile(cat + "/" + e.Name())
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".wav")
			All = append(All, Sound{Name: name, Category: cat, Data: data})
		}
	}
	load("subtle")
	load("punchy")
	load("scare")
}

// normalize turns a display name like "Ha 2" or "Ooh strong" into the
// filesystem id ("ha_2", "ooh_strong"). Accepts anything the control
// panel might send.
func normalize(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return s
}

// ByName returns a sound by display name or id; nil if not found.
func ByName(name string) *Sound {
	norm := normalize(name)
	for i := range All {
		if All[i].Name == norm {
			return &All[i]
		}
	}
	return nil
}

// ByCategory returns the slice of sounds in a category.
func ByCategory(cat string) []Sound {
	var out []Sound
	for _, s := range All {
		if s.Category == cat {
			out = append(out, s)
		}
	}
	return out
}

// Random picks a sound using weighted category selection: 70% subtle,
// 25% punchy, 5% scare. Returns nil if the catalog is empty.
func Random() *Sound {
	if len(All) == 0 {
		return nil
	}
	r := rand.Float64()
	var cat string
	switch {
	case r < 0.70:
		cat = "subtle"
	case r < 0.95:
		cat = "punchy"
	default:
		cat = "scare"
	}
	pool := ByCategory(cat)
	if len(pool) == 0 {
		// Fallback: pick anything.
		return &All[rand.Intn(len(All))]
	}
	return &pool[rand.Intn(len(pool))]
}
