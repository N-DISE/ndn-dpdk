package fetch

/*
#include "../../csrc/fetch/logic.h"
*/
import "C"
import (
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
)

// Logic implements fetcher congestion control and scheduling logic.
type Logic C.FetchLogic

func (fl *Logic) ptr() *C.FetchLogic {
	return (*C.FetchLogic)(fl)
}

// Init initializes the logic and allocates data structures.
func (fl *Logic) Init(windowCapacity int, socket eal.NumaSocket) {
	C.FetchWindow_Init(&fl.win, C.uint32_t(windowCapacity), C.int(socket.ID()))
	C.RttEst_Init(&fl.rtte)
	C.TcpCubic_Init(&fl.ca)
	C.FetchLogic_Init_(fl.ptr())
}

// Reset resets this to initial state.
func (fl *Logic) Reset() {
	C.MinSched_Close(fl.sched)
	*fl = Logic{win: fl.win}
	fl.win.loSegNum, fl.win.hiSegNum = 0, 0
	C.RttEst_Init(&fl.rtte)
	C.TcpCubic_Init(&fl.ca)
	C.FetchLogic_Init_(fl.ptr())
}

// Close deallocates data structures.
func (fl *Logic) Close() error {
	C.MinSched_Close(fl.sched)
	C.FetchWindow_Free(&fl.win)
	return nil
}

// SetFinalSegNum assigns (inclusive) final segment number.
func (fl *Logic) SetFinalSegNum(segNum uint64) {
	C.FetchLogic_SetFinalSegNum(fl.ptr(), C.uint64_t(segNum))
}

// Finished determines if all segments have been fetched.
func (fl *Logic) Finished() bool {
	return bool(C.FetchLogic_Finished(fl.ptr()))
}
