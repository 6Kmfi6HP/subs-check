package proxies

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beck-8/subs-check/config"
)

func TestSubscriptionTransportUsesConfiguredSOCKS5(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("through-socks5"))
	}))
	defer target.Close()

	targetAddr := target.Listener.Addr().String()
	proxyAddr, closeProxy := startTestSOCKS5(t, targetAddr)
	defer closeProxy()

	oldProxy := config.GlobalConfig.Socks5Proxy
	oldDNS := config.GlobalConfig.DNS.Enable
	t.Cleanup(func() {
		config.GlobalConfig.Socks5Proxy = oldProxy
		config.GlobalConfig.DNS.Enable = oldDNS
	})
	config.GlobalConfig.Socks5Proxy = proxyAddr
	config.GlobalConfig.DNS.Enable = true

	transport, err := newSubscriptionTransport(time.Second)
	if err != nil {
		t.Fatalf("newSubscriptionTransport: %v", err)
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get("http://subscription.invalid/")
	if err != nil {
		t.Fatalf("GET through SOCKS5: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "through-socks5" {
		t.Fatalf("response = %q, want through-socks5", got)
	}
}

func startTestSOCKS5(t *testing.T, targetAddr string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if err := serveTestSOCKS5(conn, targetAddr); err != nil {
			t.Errorf("SOCKS5 server: %v", err)
		}
	}()
	return "socks5://" + listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func serveTestSOCKS5(conn net.Conn, targetAddr string) error {
	reader := bufio.NewReader(conn)
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(reader, greeting); err != nil {
		return err
	}
	if greeting[0] != 5 {
		return fmt.Errorf("version = %d", greeting[0])
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return err
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	if header[0] != 5 || header[1] != 1 {
		return fmt.Errorf("request = %v", header[:2])
	}
	var addressLen int
	switch header[3] {
	case 1:
		addressLen = 4
	case 3:
		length := make([]byte, 1)
		if _, err := io.ReadFull(reader, length); err != nil {
			return err
		}
		addressLen = int(length[0])
	case 4:
		addressLen = 16
	default:
		return fmt.Errorf("address type = %d", header[3])
	}
	address := make([]byte, addressLen+2)
	if _, err := io.ReadFull(reader, address); err != nil {
		return err
	}

	target, err := net.Dial("tcp", targetAddr)
	if err != nil {
		return err
	}
	defer target.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	errs := make(chan error, 2)
	go func() { _, err := io.Copy(target, reader); errs <- err }()
	go func() { _, err := io.Copy(conn, target); errs <- err }()
	return <-errs
}
