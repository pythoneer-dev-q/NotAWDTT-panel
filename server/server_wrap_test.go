package server

import (
	"bytes"
	"net"
	"testing"
	"time"
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

func TestWrappedListenerPacketRoundTrip(t *testing.T) {
	const password = "batch-round-trip"
	keys := newWrapKeyStore()
	if err := keys.SetPasswords("", []string{password}); err != nil {
		t.Fatal(err)
	}
	listener, err := listenWrapped(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverAddr := listener.Addr().(*net.UDPAddr)
	client, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	key, err := deriveWrapKey(password)
	if err != nil {
		t.Fatal(err)
	}
	request := []byte("request")
	wire, err := obfsWrapPacket(key, request, NewObfsConfig(), NewObfsState())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(wire); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.PacketConn, 1)
	go func() {
		conn, _, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	var conn net.PacketConn
	select {
	case conn = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("wrapped packet was not accepted")
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	n, remote, err := conn.ReadFrom(buf)
	if err != nil || !bytes.Equal(buf[:n], request) {
		t.Fatalf("wrapped read: n=%d err=%v data=%q", n, err, buf[:n])
	}
	response := []byte("response")
	if _, err := conn.WriteTo(response, remote); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err = client.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, 2048)
	m, err := obfsUnwrapPacket(key, buf[:n], plain)
	if err != nil || !bytes.Equal(plain[:m], response) {
		t.Fatalf("wrapped write: n=%d err=%v data=%q", m, err, plain[:m])
	}
}
