package socketface_test

import (
	"fmt"
	"net"
	"testing"

	"ndn-dpdk/iface"
	"ndn-dpdk/iface/faceuri"
	"ndn-dpdk/iface/ifacetestfixture"
	"ndn-dpdk/iface/socketface"
)

func TestStream(t *testing.T) {
	connA, connB := net.Pipe()

	faceA := socketface.New(connA, socketface.Config{
		Mempools:    faceMempools,
		RxMp:        directMp,
		RxqCapacity: 64,
		TxqCapacity: 64,
	})
	faceB := socketface.New(connB, socketface.Config{
		Mempools:    faceMempools,
		RxMp:        directMp,
		RxqCapacity: 64,
		TxqCapacity: 64,
	})
	defer faceA.Close()
	defer faceB.Close()

	fixture := ifacetestfixture.New(t, faceA, socketface.NewRxGroup(faceA), faceB)
	fixture.RunTest()
	fixture.CheckCounters()
}

func TestTcp(t *testing.T) {
	assert, require := makeAR(t)

	addr, e := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	require.NoError(e)
	listener, e := net.ListenTCP("tcp", addr)
	require.NoError(e)
	defer listener.Close()
	*addr = *listener.Addr().(*net.TCPAddr)

	remoteUri := faceuri.MustParse(fmt.Sprintf("tcp4://127.0.0.1:%d", addr.Port))
	face, e := socketface.NewFromUri(remoteUri, nil, socketface.Config{
		Mempools:    faceMempools,
		RxMp:        directMp,
		RxqCapacity: 64,
		TxqCapacity: 64,
	})
	require.NoError(e)
	defer face.Close()

	assert.Equal(iface.FaceKind_Socket, face.GetFaceId().GetKind())
	assert.Equal(fmt.Sprintf("tcp4://%s", face.GetConn().LocalAddr()), face.GetLocalUri().String())
	assert.Equal(fmt.Sprintf("tcp4://127.0.0.1:%d", addr.Port), face.GetRemoteUri().String())
}
