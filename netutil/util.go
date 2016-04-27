package testutil

import (
	"testing"

	peer "github.com/ipfs/go-libp2p-peer"
	bhost "github.com/ipfs/go-libp2p/p2p/host/basic"
	metrics "github.com/ipfs/go-libp2p/p2p/metrics"
	inet "github.com/ipfs/go-libp2p/p2p/net"
	swarm "github.com/ipfs/go-libp2p/p2p/net/swarm"
	tu "github.com/ipfs/go-libp2p/testutil"

	ma "github.com/jbenet/go-multiaddr"
	context "golang.org/x/net/context"
)

func GenSwarmNetwork(t *testing.T, ctx context.Context) *swarm.Network {
	p := tu.RandPeerNetParamsOrFatal(t)
	ps := peer.NewPeerstore()
	ps.AddPubKey(p.ID, p.PubKey)
	ps.AddPrivKey(p.ID, p.PrivKey)
	n, err := swarm.NewNetwork(ctx, []ma.Multiaddr{p.Addr}, p.ID, ps, metrics.NewBandwidthCounter())
	if err != nil {
		t.Fatal(err)
	}
	ps.AddAddrs(p.ID, n.ListenAddresses(), peer.PermanentAddrTTL)
	return n
}

func DivulgeAddresses(a, b inet.Network) {
	id := a.LocalPeer()
	addrs := a.Peerstore().Addrs(id)
	b.Peerstore().AddAddrs(id, addrs, peer.PermanentAddrTTL)
}

func GenHostSwarm(t *testing.T, ctx context.Context) *bhost.BasicHost {
	n := GenSwarmNetwork(t, ctx)
	return bhost.New(n)
}

var RandPeerID = tu.RandPeerID
