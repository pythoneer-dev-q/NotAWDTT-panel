package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// newNodeHTTPClient uses normal CA verification unless an explicit SPKI pin
// was supplied for a panel with a private/self-signed certificate.
func newNodeHTTPClient(pin string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if pin != "" {
		if !strings.HasPrefix(pin, "sha256//") {
			return nil, fmt.Errorf("unsupported certificate pin")
		}
		want, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pin, "sha256//"))
		if err != nil || len(want) != sha256.Size {
			return nil, fmt.Errorf("invalid certificate pin")
		}
		transport.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // VerifyConnection enforces the pinned public key.
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return fmt.Errorf("panel certificate missing")
				}
				got := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
				if subtle.ConstantTimeCompare(got[:], want) != 1 {
					return fmt.Errorf("panel certificate key changed")
				}
				return nil
			},
		}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}, nil
}

func localNodeCertSHA256() string {
	dir := os.Getenv("WDTT_CONFIG_DIR")
	if dir == "" {
		dir = "/etc/wdtt-node"
	}
	data, err := os.ReadFile(filepath.Join(dir, "dtls-cert.pem"))
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:])
}
