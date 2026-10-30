package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"billiejeanpad/sounds"
)

// ntfyMsg mirrors the JSON stream envelope produced by ntfy.sh's
// GET /{topic}/json endpoint.
type ntfyMsg struct {
	ID      string `json:"id"`
	Time    int64  `json:"time"`
	Event   string `json:"event"` // "open", "keepalive", or "message"
	Topic   string `json:"topic"`
	Message string `json:"message"` // our signed JSON lives here
}

// cmd is the inner payload we push into ntfy message bodies. Ts and Sig
// are required on every operator-sent command and verified before any
// side-effect runs.
type cmd struct {
	Cmd      string `json:"cmd"`
	Sound    string `json:"sound,omitempty"`
	Category string `json:"category,omitempty"`
	Random   bool   `json:"random,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Preset   string `json:"preset,omitempty"`
	Ts       int64  `json:"ts"`
	Sig      string `json:"sig"`
}

// heartbeat is a signed roster announcement. The web controller verifies it
// before showing a machine, which prevents unrelated ntfy publishers from
// polluting the machine list.
type heartbeat struct {
	Host    string `json:"host"`
	Version string `json:"v"`
	Ts      int64  `json:"ts"`
	Timer   bool   `json:"timer"`
	Preset  string `json:"preset"`
	Sig     string `json:"sig"`
}

// subscribeLoop keeps a long-lived GET stream open on the comma-joined
// topics. Reconnects with exponential backoff on failure. Dispatches
// each decoded message through onMsg.
func subscribeLoop(ctx context.Context, topics []string, onMsg func(ntfyMsg)) {
	url := BaseURL + "/" + strings.Join(topics, ",") + "/json"
	logf("subscribe: %s", url)

	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := subscribeOnce(ctx, url, onMsg)
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		if err != nil && ctx.Err() == nil {
			logf("subscribe reconnect in %v: %v", backoff, err)
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

type scanResult struct {
	line []byte
	err  error
}

func subscribeOnce(ctx context.Context, url string, onMsg func(ntfyMsg)) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy subscribe returned HTTP %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	results := make(chan scanResult, 1)
	done := make(chan struct{})
	defer close(done)

	go func() {
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			select {
			case results <- scanResult{line: line}:
			case <-done:
				return
			}
		}
		select {
		case results <- scanResult{err: sc.Err()}:
		case <-done:
		}
	}()

	// ntfy emits keepalives on an active stream. A connection that produces
	// no line for two minutes is considered half-open and is re-established.
	idle := time.NewTimer(2 * time.Minute)
	defer idle.Stop()
	for {
		select {
		case result := <-results:
			if result.line == nil {
				if result.err != nil {
					return result.err
				}
				return fmt.Errorf("ntfy subscription closed")
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(2 * time.Minute)

			var m ntfyMsg
			if err := json.Unmarshal(result.line, &m); err != nil {
				continue
			}
			if m.Event == "message" {
				onMsg(m)
			}
		case <-idle.C:
			return fmt.Errorf("ntfy stream idle timeout")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publish posts a raw body to a topic. Fire-and-forget: errors are
// logged by the caller when it cares.
func publish(topic, body string) error {
	req, _ := http.NewRequest("POST", BaseURL+"/"+topic, strings.NewReader(body))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy publish returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// onNtfyMessage is the dispatch for every signed command. verify() runs
// before any side effect — invalid messages are logged and dropped.
func onNtfyMessage(m ntfyMsg) {
	var c cmd
	if err := json.Unmarshal([]byte(m.Message), &c); err != nil {
		logf("drop: bad json: %v", err)
		return
	}
	if err := verify(c); err != nil {
		logf("drop: %v (cmd=%q, ts=%d)", err, c.Cmd, c.Ts)
		return
	}

	switch c.Cmd {
	case "play":
		var s *sounds.Sound
		switch {
		case c.Sound != "":
			s = sounds.ByName(c.Sound)
		case c.Category != "":
			pool := sounds.ByCategory(c.Category)
			if len(pool) > 0 {
				s = &pool[rand.Intn(len(pool))]
			}
		case c.Random:
			s = sounds.Random()
		}
		if s == nil {
			logf("play: no match (sound=%q cat=%q random=%v)",
				c.Sound, c.Category, c.Random)
			return
		}
		logf("play: %s/%s", s.Category, s.Name)
		playWAV(s.Data)
	case "stop":
		logf("stop")
		stopAudio()
	case "ping":
		logf("ping -> heartbeat now")
		sendHeartbeat()
	case "timer":
		if c.Enabled != nil {
			logf("timer: enabled=%v", *c.Enabled)
			timer.SetEnabled(*c.Enabled)
		}
		if c.Preset != "" {
			logf("timer: preset=%s", c.Preset)
			timer.SetPreset(c.Preset)
		}
		// Push a fresh heartbeat so the panel reflects the new state
		// immediately instead of waiting up to 5 min.
		go sendHeartbeat()
	case "uninstall":
		logf("uninstall via remote command")
		go uninstall()
	default:
		logf("drop: unknown cmd %q", c.Cmd)
	}
}

// heartbeatLoop announces at startup and every six hours. Routine discovery
// comes from a signed ping when the controller opens. This stays well below
// ntfy.sh's public daily message quota, even when many office machines share
// one public IP address.
func heartbeatLoop(ctx context.Context) {
	sendHeartbeat()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			sendHeartbeat()
		case <-ctx.Done():
			return
		}
	}
}

func sendHeartbeat() {
	h := heartbeat{
		Host:    hostID(),
		Version: Version,
		Ts:      time.Now().Unix(),
		Timer:   timer.Enabled(),
		Preset:  timer.Preset(),
	}
	h.Sig = signHeartbeat(h)
	body, _ := json.Marshal(h)
	if err := publish(topicPrefix()+"-roster", string(body)); err != nil {
		logf("heartbeat failed: %v", err)
	}
}

func hostID() string {
	h, _ := os.Hostname()
	return strings.ToLower(h)
}
