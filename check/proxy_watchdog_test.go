package check

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/constant"
)

type countingProxy struct {
	constant.Proxy
	closes atomic.Int32
}

func (p *countingProxy) Close() error {
	p.closes.Add(1)
	return nil
}

func startWatchdog(lifetime time.Duration) (*ProxyClient, *countingProxy, <-chan struct{}) {
	proxy := &countingProxy{}
	pc := &ProxyClient{proxy: proxy}
	pc.ctx, pc.cancel = context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pc.watchdog("watchdog-test", lifetime)
		close(done)
	}()
	return pc, proxy, done
}

// After lifetime the proxy must be closed repeatedly, and closing stops on Close.
func TestWatchdogKeepsClosingUntilClose(t *testing.T) {
	pc, proxy, done := startWatchdog(50 * time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for proxy.closes.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("watchdog closed proxy %d times, want repeated closes", proxy.closes.Load())
		}
		time.Sleep(50 * time.Millisecond)
	}

	pc.cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not stop after Close")
	}
}

func TestWatchdogIdleWhenClosedInTime(t *testing.T) {
	pc, proxy, done := startWatchdog(time.Hour)

	pc.cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not stop after Close")
	}
	if n := proxy.closes.Load(); n != 0 {
		t.Fatalf("watchdog closed proxy %d times before lifetime", n)
	}
}
