package fwdptest

import (
	"testing"
	"time"

	"github.com/usnistgov/ndn-dpdk/app/fwdp"
	"github.com/usnistgov/ndn-dpdk/iface/intface"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

func TestDataWrongName(t *testing.T) {
	assert, _ := makeAR(t)
	fixture := NewFixture(t)

	face1, face2 := intface.MustNew(), intface.MustNew()
	collect1, collect2 := intface.Collect(face1), intface.Collect(face2)
	fixture.SetFibEntry("/B", "multicast", face2.ID)

	face1.Tx <- ndn.MakeInterest("/B/1")
	fixture.StepDelay()
	assert.Equal(1, collect2.Count())

	face2.Tx <- ndn.MakeData(collect2.Get(-1).Interest, "/B/2") // name does not match
	fixture.StepDelay()
	assert.Equal(0, collect1.Count())

	assert.Equal(uint64(1), fixture.SumCounter(func(fwd *fwdp.Fwd) uint64 {
		return fwd.Pit().Counters().NDataMiss
	}))
}

func TestDataLongerName(t *testing.T) {
	assert, _ := makeAR(t)
	fixture := NewFixture(t)

	face1, face2 := intface.MustNew(), intface.MustNew()
	collect1, collect2 := intface.Collect(face1), intface.Collect(face2)
	fixture.SetFibEntry("/B", "multicast", face2.ID)

	face1.Tx <- ndn.MakeInterest("/B/1") // no CanBePrefix
	fixture.StepDelay()
	assert.Equal(1, collect2.Count())

	face2.Tx <- ndn.MakeData(collect2.Get(-1).Interest, "/B/1/Z") // name has suffix
	fixture.StepDelay()
	assert.Equal(0, collect1.Count())

	assert.Equal(uint64(1), fixture.SumCounter(func(fwd *fwdp.Fwd) uint64 {
		return fwd.Pit().Counters().NDataMiss
	}))
}

func TestDataZeroFreshnessPeriod(t *testing.T) {
	assert, _ := makeAR(t)
	fixture := NewFixture(t)

	face1, face2 := intface.MustNew(), intface.MustNew()
	collect1, collect2 := intface.Collect(face1), intface.Collect(face2)
	fixture.SetFibEntry("/B", "multicast", face2.ID)

	face1.Tx <- ndn.MakeInterest("/B/1", ndn.MustBeFreshFlag) // has MustBeFresh
	fixture.StepDelay()
	assert.Equal(1, collect2.Count())

	face2.Tx <- ndn.MakeData(collect2.Get(-1).Interest, 0*time.Millisecond) // no FreshnessPeriod
	fixture.StepDelay()
	assert.Equal(0, collect1.Count())

	assert.Equal(uint64(1), fixture.SumCounter(func(fwd *fwdp.Fwd) uint64 {
		return fwd.Pit().Counters().NDataMiss
	}))
}

func TestNackWrongName(t *testing.T) {
	assert, _ := makeAR(t)
	fixture := NewFixture(t)

	face1, face2 := intface.MustNew(), intface.MustNew()
	collect1, collect2 := intface.Collect(face1), intface.Collect(face2)
	fixture.SetFibEntry("/B", "multicast", face2.ID)

	face1.Tx <- ndn.MakeInterest("/B/1", ndn.NonceFromUint(0xdb22330b))
	fixture.StepDelay()
	assert.Equal(1, collect2.Count())

	face2.Tx <- ndn.MakeNack(ndn.MakeInterest("/B/2", ndn.NonceFromUint(0xdb22330b)), collect2.Get(-1).Lp)
	fixture.StepDelay()
	assert.Equal(0, collect1.Count())

	assert.Equal(uint64(1), fixture.SumCounter(func(fwd *fwdp.Fwd) uint64 {
		return fwd.Pit().Counters().NNackMiss
	}))
}

func TestNackWrongNonce(t *testing.T) {
	assert, _ := makeAR(t)
	fixture := NewFixture(t)

	face1, face2 := intface.MustNew(), intface.MustNew()
	collect1, collect2 := intface.Collect(face1), intface.Collect(face2)
	fixture.SetFibEntry("/B", "multicast", face2.ID)

	face1.Tx <- ndn.MakeInterest("/B/1", ndn.NonceFromUint(0x19c3e8b8))
	fixture.StepDelay()
	assert.Equal(1, collect2.Count())

	face2.Tx <- ndn.MakeNack(ndn.MakeInterest("/B/1", ndn.NonceFromUint(0xf4d9aad1)), collect2.Get(-1).Lp)
	fixture.StepDelay()
	assert.Equal(0, collect1.Count())

	assert.Equal(uint64(1), fixture.SumCounter(func(fwd *fwdp.Fwd) uint64 {
		return fwd.Counters().NNackMismatch
	}))
}
