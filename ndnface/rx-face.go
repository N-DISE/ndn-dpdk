package ndnface

/*
#include "rx-face.h"
*/
import "C"
import (
	"fmt"
	"unsafe"

	"ndn-dpdk/dpdk"
	"ndn-dpdk/ndn"
)

type RxFace struct {
	c *C.RxFace
}

func NewRxFace(q dpdk.EthRxQueue) (face RxFace) {
	face.c = (*C.RxFace)(C.calloc(1, C.sizeof_RxFace))
	face.c.port = C.uint16_t(q.GetPort())
	face.c.queue = C.uint16_t(q.GetQueue())
	return face
}

func (face RxFace) Close() {
	C.free(unsafe.Pointer(face.c))
}

func (face RxFace) RxBurst(pkts []ndn.Packet) int {
	if len(pkts) == 0 {
		return 0
	}
	res := C.RxFace_RxBurst(face.c, (**C.struct_rte_mbuf)(unsafe.Pointer(&pkts[0])),
		C.uint16_t(len(pkts)))
	return int(res)
}

type RxFaceCounters struct {
	NInterests uint64
	NData      uint64
	NNacks     uint64

	NFrames uint64 // total L2 frames
	NOctets uint64
}

func (face RxFace) GetCounters() (cnt RxFaceCounters) {
	cnt.NInterests = uint64(face.c.nInterestPkts)
	cnt.NData = uint64(face.c.nDataPkts)

	cnt.NFrames = uint64(face.c.nFrames)

	return cnt
}

func (cnt RxFaceCounters) String() string {
	return fmt.Sprintf(
		"L3 %dI %dD %dN, L2 %dfrm %db",
		cnt.NInterests, cnt.NData, 0, cnt.NFrames, 0)
}
