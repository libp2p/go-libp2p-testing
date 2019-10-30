package ttransport

import (
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/transport"

	ma "github.com/multiformats/go-multiaddr"
)

var Subtests = []interface{}{
	SubtestProtocols,
	SubtestBasic,
	SubtestCancel,
	SubtestPingPong,

	// Stolen from the stream muxer test suite.
	SubtestStress1Conn1Stream1Msg,
	SubtestStress1Conn1Stream100Msg,
	SubtestStress1Conn100Stream100Msg,
	SubtestStress50Conn10Stream50Msg,
	SubtestStress1Conn1000Stream10Msg,
	SubtestStress1Conn100Stream100Msg10MB,
	SubtestStreamOpenStress,
	SubtestStreamReset,
}

func getFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}

func SubtestTransport(t *testing.T, ta, tb transport.Transport, addr string, peerA peer.ID) {
	SubtestTransportThrottled(t, ta, tb, addr, peerA, 0)
}

func SubtestTransportThrottled(t *testing.T, ta, tb transport.Transport, addr string, peerA peer.ID, throttle time.Duration) {
	maddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		t.Fatal(err)
	}

	rateLimit := NewRateLimiter(throttle)

	for _, f := range Subtests {
		switch v := f.(type) {
		case func(t *testing.T, ta, tb transport.Transport, maddr ma.Multiaddr, peerA peer.ID, rateLimit RateLimiter):
			t.Run(getFunctionName(v), func(t *testing.T) {
				v(t, ta, tb, maddr, peerA, rateLimit)
			})
		case func(t *testing.T, ta, tb transport.Transport, maddr ma.Multiaddr, peerA peer.ID):
			t.Run(getFunctionName(v), func(t *testing.T) {
				v(t, ta, tb, maddr, peerA)
			})
		}
	}
}
