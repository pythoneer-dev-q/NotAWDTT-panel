package server

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNodeHTTPClientCertificatePin(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	cert, err := x509.ParseCertificate(srv.TLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	pin := "sha256//" + base64.StdEncoding.EncodeToString(sum[:])
	client, err := newNodeHTTPClient(pin)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("valid pin rejected: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	wrong := "sha256//" + base64.StdEncoding.EncodeToString(make([]byte, sha256.Size))
	client, err = newNodeHTTPClient(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err = client.Get(srv.URL); err == nil {
		resp.Body.Close()
		t.Fatal("wrong pin accepted")
	}
}
