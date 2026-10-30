package server

import (
	"net"
	"testing"
	"time"
)

func TestRelayFailuresExpireAndNeverPermanentlyBlock(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 49152}
	oldBase := dtlsHandshakeTimeout
	dtlsHandshakeTimeout = 30 * time.Second
	defer func() { dtlsHandshakeTimeout = oldBase; relayNoteHandshakeOK(addr) }()
	for i := 0; i < 4; i++ {
		relayNoteHandshakeFail(addr)
	}
	if got := relayHandshakeTimeoutFor(addr); got != 30*time.Second {
		t.Fatalf("retry timeout shortened after failures: %s", got)
	}
	relayFailMu.Lock()
	state := relayFailCounts[addr.String()]
	state.last = time.Now().Add(-relayFailTTL - time.Second)
	relayFailCounts[addr.String()] = state
	relayFailMu.Unlock()
	if got := relayHandshakeTimeoutFor(addr); got != 30*time.Second {
		t.Fatalf("relay did not recover after TTL: %s", got)
	}
	relayNoteHandshakeFail(addr)
	relayFailMu.Lock()
	count := relayFailCounts[addr.String()].count
	relayFailMu.Unlock()
	if count != 1 {
		t.Fatalf("expired failure count persisted: %d", count)
	}
}
