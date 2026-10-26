package server

import (
	"net"
	"testing"
)

func TestWrappedListenerStartsBeforeNodePasswordsArrive(t *testing.T) {
	keys := newWrapKeyStore()
	listener, err := listenWrapped(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, keys)
	if err != nil {
		t.Fatalf("listener must start before the first node sync: %v", err)
	}
	defer listener.Close()
	wrapped, ok := listener.(*wrapPacketListener)
	if !ok || wrapped.keys != keys {
		t.Fatal("listener is not using the live node key store")
	}
	if keys.Count() != 0 {
		t.Fatal("new node unexpectedly has passwords")
	}
	if err := keys.SetPasswords("", []string{"synced-password"}); err != nil {
		t.Fatal(err)
	}
	if keys.Count() != 1 {
		t.Fatal("listener did not receive the synchronized key store")
	}
}
