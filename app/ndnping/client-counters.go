package ndnping

/*
#include "client-rx.h"
#include "client-tx.h"
#include "token.h"
*/
import "C"
import (
	"fmt"
	"math"
	"time"
	"unsafe"

	"ndn-dpdk/core/running_stat"
	"ndn-dpdk/dpdk"
)

type ClientPacketCounters struct {
	NInterests uint64
	NData      uint64
	NNacks     uint64
}

func (cnt ClientPacketCounters) ComputeDataRatio() float64 {
	return float64(cnt.NData) / float64(cnt.NInterests)
}

func (cnt ClientPacketCounters) ComputeNackRatio() float64 {
	return float64(cnt.NNacks) / float64(cnt.NInterests)
}

func (cnt ClientPacketCounters) String() string {
	return fmt.Sprintf("%dI %dD(%0.2f%%) %dN(%0.2f%%)",
		cnt.NInterests,
		cnt.NData, cnt.ComputeDataRatio()*100.0,
		cnt.NNacks, cnt.ComputeNackRatio()*100.0)
}

type ClientRttCounters struct {
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	Stdev time.Duration
}

func (cnt *ClientRttCounters) Set(s running_stat.RunningStat) {
	durationUnit := dpdk.GetNanosInTscUnit() * math.Pow(2.0, float64(C.PING_TIMING_PRECISION))
	toDuration := func(x float64) time.Duration {
		if math.IsNaN(x) {
			return 0
		}
		return time.Duration(x * durationUnit)
	}

	cnt.Min = toDuration(s.Min())
	cnt.Max = toDuration(s.Max())
	cnt.Avg = toDuration(s.Mean())
	cnt.Stdev = toDuration(s.Stdev())
}

func (cnt ClientRttCounters) String() string {
	return fmt.Sprintf("%0.3f/%0.3f/%0.3f/%0.3fms",
		float64(cnt.Min)/float64(time.Millisecond), float64(cnt.Avg)/float64(time.Millisecond),
		float64(cnt.Max)/float64(time.Millisecond), float64(cnt.Stdev)/float64(time.Millisecond))
}

type ClientPatternCounters struct {
	ClientPacketCounters
	Rtt         ClientRttCounters
	NRttSamples uint64
}

func (cnt ClientPatternCounters) String() string {
	return fmt.Sprintf("%s rtt=%s(%dsamp)",
		cnt.ClientPacketCounters, cnt.Rtt, cnt.NRttSamples)
}

type ClientCounters struct {
	ClientPacketCounters
	NAllocError uint64
	Rtt         ClientRttCounters
	PerPattern  []ClientPatternCounters
}

func (cnt ClientCounters) String() string {
	s := fmt.Sprintf("%s %dalloc-error rtt=%s", cnt.ClientPacketCounters, cnt.NAllocError, cnt.Rtt)
	for i, pcnt := range cnt.PerPattern {
		s += fmt.Sprintf(", pattern(%d) %s", i, pcnt)
	}
	return s
}

// Read counters.
func (client *Client) ReadCounters() (cnt ClientCounters) {
	rttCombined := running_stat.New()
	for i := 0; i < int(client.c.nPatterns); i++ {
		crP := client.c.pattern[i]
		ctP := client.Tx.c.pattern[i]
		rtt := running_stat.FromPtr(unsafe.Pointer(&crP.rtt))

		var pcnt ClientPatternCounters
		pcnt.NInterests = uint64(ctP.nInterests)
		pcnt.NData = uint64(crP.nData)
		pcnt.NNacks = uint64(crP.nNacks)
		pcnt.NRttSamples = rtt.Len64()
		pcnt.Rtt.Set(rtt)
		cnt.PerPattern = append(cnt.PerPattern, pcnt)

		cnt.NInterests += pcnt.NInterests
		cnt.NData += pcnt.NData
		cnt.NNacks += pcnt.NNacks
		rttCombined = running_stat.Combine(rttCombined, rtt)
	}

	cnt.NAllocError = uint64(client.Tx.c.nAllocError)
	cnt.Rtt.Set(rttCombined)
	return cnt
}

// Clear counters. Both RX and TX threads should be stopped before calling this,
// otherwise race conditions may occur.
func (client *Client) ClearCounters() {
	nPatterns := int(client.c.nPatterns)
	for i := 0; i < nPatterns; i++ {
		client.clearCounter(i)
	}
}

func (client *Client) clearCounter(index int) {
	client.c.pattern[index].nData = 0
	client.c.pattern[index].nNacks = 0
	rtt := running_stat.FromPtr(unsafe.Pointer(&client.c.pattern[index].rtt))
	rtt.Clear(true)
	client.Tx.c.pattern[index].nInterests = 0
}
