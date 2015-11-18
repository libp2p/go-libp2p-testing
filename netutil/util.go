package testutil

import (
	"testing"

	bhost "github.com/ipfs/go-libp2p/p2p/host/basic"
	metrics "github.com/ipfs/go-libp2p/p2p/metrics"
	inet "github.com/ipfs/go-libp2p/p2p/net"
	swarm "github.com/ipfs/go-libp2p/p2p/net/swarm"
	peer "github.com/ipfs/go-libp2p/p2p/peer"
	tu "github.com/ipfs/go-libp2p/testutil"

	ma "gx/QmVUi2ncqnU48zsPgR1rQosDGwY3SSZ1Ndp33j33YjXdsj/go-multiaddr"
	context "gx/QmacZi9WygGK7Me8mH53pypyscHzU386aUZXpr28GZgUct/context"
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
