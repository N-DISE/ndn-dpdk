package iface

/*
#include "../csrc/iface/rxloop.h"
*/
import "C"
import (
	"io"
	"math"
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/core/cptr"
	"github.com/usnistgov/ndn-dpdk/core/urcu"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ealthread"
	"github.com/usnistgov/ndn-dpdk/ndni"
	"go.uber.org/zap"
)

// RoleRx is the thread role for RxLoop.
const RoleRx = "RX"

// RxGroup is a receive channel for faces.
// An RxGroup may serve multiple faces; a face may have multiple RxGroups.
type RxGroup interface {
	eal.WithNumaSocket

	// RxGroup returns *C.RxGroup pointer and description.
	RxGroup() (ptr unsafe.Pointer, desc string)
}

// RxGroupSingleFace indicates this kind of RxGroup can serve at most one face.
type RxGroupSingleFace interface {
	RxGroup
	RxGroupIsSingleFace()
}

// RxLoop is the input thread that processes incoming packets on a set of RxGroups.
// Functions are non-thread-safe.
type RxLoop interface {
	eal.WithNumaSocket
	ealthread.ThreadWithRole
	ealthread.ThreadWithLoadStat
	WithInputDemuxes
	io.Closer

	CountRxGroups() int
	Add(rxg RxGroup)
	Remove(rxg RxGroup)
}

// NewRxLoop creates an RxLoop.
func NewRxLoop(socket eal.NumaSocket) RxLoop {
	rxl := &rxLoop{
		c:      eal.Zmalloc[C.RxLoop]("RxLoop", C.sizeof_RxLoop, socket),
		socket: socket,
	}
	rxl.ThreadWithCtrl = ealthread.NewThreadWithCtrl(
		cptr.Func0.C(unsafe.Pointer(C.RxLoop_Run), rxl.c),
		unsafe.Pointer(&rxl.c.ctrl),
	)
	rxLoopThreads[rxl] = true
	logger.Info("RxLoop created",
		zap.Uintptr("rxl-ptr", uintptr(unsafe.Pointer(rxl.c))),
	)
	return rxl
}

type rxLoop struct {
	ealthread.ThreadWithCtrl
	c      *C.RxLoop
	socket eal.NumaSocket
	nRxgs  int
}

func (rxl *rxLoop) NumaSocket() eal.NumaSocket {
	return rxl.socket
}

func (rxl *rxLoop) ThreadRole() string {
	return RoleRx
}

func (rxl *rxLoop) Close() error {
	rxl.Stop()
	delete(rxLoopThreads, rxl)
	logger.Info("RxLoop closed",
		zap.Uintptr("rxl-ptr", uintptr(unsafe.Pointer(rxl.c))),
	)
	eal.Free(rxl.c)
	return nil
}

func (rxl *rxLoop) DemuxOf(t ndni.PktType) *InputDemux {
	return (*InputDemux)(C.InputDemux_Of(&rxl.c.demuxes, C.PktType(t)))
}

func (rxl *rxLoop) CountRxGroups() int {
	return rxl.nRxgs
}

func (rxl *rxLoop) Add(rxg RxGroup) {
	rxgPtr, rxgDesc := rxg.RxGroup()
	logEntry := logger.With(
		zap.Uintptr("rxl-ptr", uintptr(unsafe.Pointer(rxl.c))),
		rxl.LCore().ZapField("rxl-lc"),
		zap.Uintptr("rxg-ptr", uintptr(rxgPtr)),
		zap.String("rxg", rxgDesc),
	)

	rxgC := (*C.RxGroup)(rxgPtr)
	if rxgC.rxBurst == nil {
		logEntry.Panic("RxGroup missing rxBurst")
	}

	if mapRxgRxl[rxg] != nil {
		logEntry.Panic("RxGroup is in another RxLoop")
	}
	mapRxgRxl[rxg] = rxl
	rxl.nRxgs++

	logEntry.Debug("adding RxGroup to RxLoop")
	C.cds_hlist_add_head_rcu(&rxgC.rxlNode, &rxl.c.head)
}

func (rxl *rxLoop) Remove(rxg RxGroup) {
	rxgPtr, rxgDesc := rxg.RxGroup()
	logEntry := logger.With(
		zap.Uintptr("rxl-ptr", uintptr(unsafe.Pointer(rxl.c))),
		rxl.LCore().ZapField("rxl-lc"),
		zap.Uintptr("rxg-ptr", uintptr(rxgPtr)),
		zap.String("rxg", rxgDesc),
	)

	rxgC := (*C.RxGroup)(rxgPtr)
	if mapRxgRxl[rxg] != rxl {
		logger.Panic("RxGroup is not in this RxLoop")
	}
	delete(mapRxgRxl, rxg)
	rxl.nRxgs--

	logEntry.Debug("removing RxGroup from RxLoop")
	C.cds_hlist_del_rcu(&rxgC.rxlNode)
	urcu.Barrier()
}

var (
	// ChooseRxLoop customizes RxLoop selection in ActivateRxGroup.
	// Return nil to use default algorithm.
	ChooseRxLoop = func(rxg RxGroup) RxLoop { return nil }

	rxLoopThreads = map[RxLoop]bool{}
	mapRxgRxl     = map[RxGroup]RxLoop{}
)

// ListRxLoops returns a list of RxLoops.
func ListRxLoops() (list []RxLoop) {
	for rxl := range rxLoopThreads {
		list = append(list, rxl)
	}
	return list
}

// ActivateRxGroup selects an RxLoop and adds the RxGroup to it.
// Returns chosen RxLoop.
//
// The default logic selects among existing RxLoops for the least loaded one, preferably on the
// same NUMA socket as the RxGroup. In case no RxLoop exists, one is created and launched
// automatically. This does not respect LCoreAlloc, and may panic if no LCore is available.
//
// This logic may be overridden via ChooseRxLoop.
func ActivateRxGroup(rxg RxGroup) RxLoop {
	if rxl := ChooseRxLoop(rxg); rxl != nil {
		rxl.Add(rxg)
		return rxl
	}

	socket := rxg.NumaSocket()
	if len(rxLoopThreads) == 0 {
		rxl := NewRxLoop(socket)
		if e := ealthread.AllocLaunch(rxl); e != nil {
			logger.Panic("no RxLoop available and cannot launch new RxLoop", zap.Error(e))
		}
		rxl.Add(rxg)
		return rxl
	}

	var bestRxl RxLoop
	bestScore := math.MaxInt32
	for rxl := range rxLoopThreads {
		score := rxl.CountRxGroups()
		if !socket.Match(rxl.NumaSocket()) {
			score += 1000000
		}
		if score <= bestScore {
			bestRxl, bestScore = rxl, score
		}
	}
	bestRxl.Add(rxg)
	return bestRxl
}

// DeactivateRxGroup removes the RxGroup from the owning RxLoop.
func DeactivateRxGroup(rxg RxGroup) {
	mapRxgRxl[rxg].Remove(rxg)
}
