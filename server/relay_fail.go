package server

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

// Неудачные DTLS подключения через TURN учитываются только для диагностики.
// Повторная попытка всегда получает полный timeout из настройки панели.

const (
	relayFailTTL          = time.Minute
	relayFailMapClearSize = 8000
)

type relayFailure struct {
	count int
	last  time.Time
}

var (
	relayFailMu     sync.Mutex
	relayFailCounts = map[string]relayFailure{}
)

func relayHandshakeTimeoutFor(addr net.Addr) time.Duration {
	return dtlsHandshakeTimeout
}

func relayNoteHandshakeFail(addr net.Addr) {
	if addr == nil {
		return
	}
	key := addr.String()
	relayFailMu.Lock()
	if _, known := relayFailCounts[key]; !known && len(relayFailCounts) >= relayFailMapClearSize {
		relayFailMu.Unlock()
		return
	}
	state := relayFailCounts[key]
	if time.Since(state.last) > relayFailTTL {
		state.count = 0
	}
	state.count++
	state.last = time.Now()
	relayFailCounts[key] = state
	n := state.count
	relayFailMu.Unlock()
	if n == 1 || n == 3 {
		log.Printf("[RELAY] TURN relay %s: %d DTLS handshake fail(s) within %s; retries remain allowed", key, n, relayFailTTL)
	}
}

func relayNoteHandshakeOK(addr net.Addr) {
	if addr == nil {
		return
	}
	key := addr.String()
	relayFailMu.Lock()
	delete(relayFailCounts, key)
	relayFailMu.Unlock()
}

func relayFailJanitor(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			relayFailMu.Lock()
			n := len(relayFailCounts)
			for key, state := range relayFailCounts {
				if time.Since(state.last) > relayFailTTL {
					delete(relayFailCounts, key)
				}
			}
			if n > relayFailMapClearSize {
				log.Printf("[RELAY] Cleared stale relay fail entries: %d -> %d", n, len(relayFailCounts))
			}
			relayFailMu.Unlock()
		}
	}
}
