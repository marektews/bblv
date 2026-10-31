package ftps

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// silentServer accepts connections and never answers, like a printer that
// dropped off the network after the TCP handshake.
func silentServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	return ln.Addr().String()
}

func TestStalledSessionTimesOut(t *testing.T) {
	addr := silentServer(t)
	start := time.Now()
	if _, err := dial(context.Background(), addr, "127.0.0.1", "code", 100*time.Millisecond); err == nil {
		t.Fatal("dial succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("gave up after %v", d)
	}
}

func TestCancelInterruptsSession(t *testing.T) {
	addr := silentServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	if _, err := dial(ctx, addr, "127.0.0.1", "code", time.Hour); err == nil {
		t.Fatal("dial succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("gave up after %v", d)
	}
}
