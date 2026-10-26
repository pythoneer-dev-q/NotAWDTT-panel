package panel

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"wdtt-panel/network"
)

type quietLogger struct{}

func (quietLogger) Write(p []byte) (int, error) {
	return network.NewQuietTLSLog().Write(p)
}

func panelListenAddr(cfg *PanelConfig) string {
	port := cfg.Port
	if port == 0 {
		port = 2860
	}
	listen := strings.TrimSpace(cfg.WebListen)
	if listen == "" {
		return fmt.Sprintf(":%d", port)
	}
	return net.JoinHostPort(listen, fmt.Sprintf("%d", port))
}

// An empty or IPv4 bind address must also accept reverse proxies using 127.0.0.1.
// Keep an explicit IPv6 bind available for installations that need one.
func panelListenNetwork(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "tcp"
	}
	if host == "" || host == "0.0.0.0" {
		return "tcp4"
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return "tcp4"
		}
		return "tcp6"
	}
	return "tcp"
}

func localListenerURL(listener net.Listener, scheme, path string) string {
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return scheme + "://" + listener.Addr().String() + path
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port) + path
}

func validatePanelTLS(certFile, keyFile string) error {
	certFile = strings.TrimSpace(certFile)
	keyFile = strings.TrimSpace(keyFile)
	if certFile == "" && keyFile == "" {
		return nil
	}
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("укажите оба файла: сертификат и ключ")
	}
	_, err := tls.LoadX509KeyPair(certFile, keyFile)
	return err
}

func startPanelServer(cfg *PanelConfig, handler http.Handler) error {
	addr := panelListenAddr(cfg)
	listener, err := net.Listen(panelListenNetwork(addr), addr)
	if err != nil {
		return err
	}

	certFile := strings.TrimSpace(cfg.WebCertFile)
	keyFile := strings.TrimSpace(cfg.WebKeyFile)
	scheme := "http"
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			log.Printf("Warning: SSL требует cert и key — панель без HTTPS")
		} else {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				log.Printf("Error loading certificates: %v — панель без HTTPS", err)
			} else {
				listener = network.NewMuxListener(listener, &tls.Config{
					Certificates: []tls.Certificate{cert},
					MinVersion:   tls.VersionTLS12,
				})
				scheme = "https"
			}
		}
	}

	log.Printf("WDTT Panel: %s (локально; слушает %s)", localListenerURL(listener, scheme, cfg.basePath()), listener.Addr())
	if scheme == "http" && (certFile != "" || keyFile != "") {
		log.Printf("Проверьте пути к сертификату и перезапустите панель")
	}
	log.Printf("Логин: %s / пароль по умолчанию: wdtt (смените в настройках)", cfg.Username)
	srv := &http.Server{
		Handler:  handler,
		ErrorLog: log.New(quietLogger{}, "", log.LstdFlags),
	}
	return srv.Serve(listener)
}
