package gateway

import (
	"errors"
	"time"
)

// ClientInfo contains a SHA-256 fingerprint, never key bytes or a key suffix.
// Activity is authenticated direct traffic observed during this process lifetime.
// Cloud relay traffic does not identify a direct pairing and is not inferred.
type ClientInfo struct {
	ID       string `json:"id"`
	LastUsed int64  `json:"last_used,omitempty"`
	Recent   bool   `json:"recent"`
}

func (g *Gateway) touchClientLocked(owner string, now int64) {
	if g.lastUsed == nil {
		g.lastUsed = make(map[string]int64)
	}
	g.lastUsed[owner] = now
}
func (g *Gateway) Clients() []ClientInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]ClientInfo, 0, len(g.keys))
	now := time.Now().Unix()
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		owner := watchOwner(key)
		clear(key)
		used := g.lastUsed[owner]
		result = append(result, ClientInfo{owner, used, used > 0 && now >= used && now-used <= 120})
	}
	return result
}

// RemoveClientIdle uses the durable Remove path under the same admission lock.
// Unknown IDs are harmless (idempotent). No service restart or modem command.
func (g *Gateway) RemoveClientIdle(owner string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pcm || g.earlyStreamDone != nil {
		return errors.New("音频会话进行中，请结束后再移除配对")
	}
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		match := watchOwner(key) == owner
		clear(key)
		if match {
			return g.removeLocked(encoded)
		}
	}
	return nil
}

// Called under g.mu immediately before reserving the shared PCM slot. A key
// removed after handshake authentication must not open a new audio session.
func (g *Gateway) reserveClientPCMLocked(owner string) bool {
	if g.pcm || g.closing || !g.hasClientLocked(owner) {
		return false
	}
	g.pcm = true
	return true
}
func (g *Gateway) hasClientLocked(owner string) bool {
	for _, encoded := range g.keys {
		key, _ := decode(encoded)
		match := watchOwner(key) == owner
		clear(key)
		if match {
			return true
		}
	}
	return false
}
