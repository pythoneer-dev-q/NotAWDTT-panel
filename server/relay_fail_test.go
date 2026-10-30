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
	if got := relayHandshakeTimeoutFor(addr); got != relayHandshakeMaxWait {
		t.Fatalf("unexpected handshake timeout after failures: %s", got)
	}
	relayFailMu.Lock()
	state := relayFailCounts[addr.String()]
	state.last = time.Now().Add(-relayFailTTL - time.Second)
	relayFailCounts[addr.String()] = state
	relayFailMu.Unlock()
	if got := relayHandshakeTimeoutFor(addr); got != relayHandshakeMaxWait {
		t.Fatalf("unexpected handshake timeout after TTL: %s", got)
	}
	relayNoteHandshakeFail(addr)
	relayFailMu.Lock()
	count := relayFailCounts[addr.String()].count
	relayFailMu.Unlock()
	if count != 1 {
		t.Fatalf("expired failure count persisted: %d", count)
	}
}

func TestRelayHandshakeTimeoutRespectsShorterConfiguredLimit(t *testing.T) {
	oldBase := dtlsHandshakeTimeout
	dtlsHandshakeTimeout = 4 * time.Second
	defer func() { dtlsHandshakeTimeout = oldBase }()
	if got := relayHandshakeTimeoutFor(nil); got != 4*time.Second {
		t.Fatalf("configured timeout ignored: %s", got)
	}
}
