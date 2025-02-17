package fetch

/*
#include "../../csrc/fetch/fetcher.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/ringbuffer"
	"github.com/usnistgov/ndn-dpdk/iface"
	"github.com/usnistgov/ndn-dpdk/ndn/an"
	"github.com/usnistgov/ndn-dpdk/ndn/segmented"
	"github.com/usnistgov/ndn-dpdk/ndni"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

var (
	lastTaskContextID int
	taskContextByID   = map[int]*TaskContext{}
	taskContextLock   sync.RWMutex
)

// TaskContext provides contextual information about an active fetch task.
type TaskContext struct {
	d        TaskDef
	id       int
	fetcher  *Fetcher
	w        *worker
	ts       *taskSlot
	stopping chan struct{}
}

// Counters returns congestion control and scheduling counters.
func (task *TaskContext) Counters() Counters {
	return task.ts.Logic().Counters()
}

// Stop aborts/stops the fetch task.
// This should be called even if the fetch task has succeeded.
func (task *TaskContext) Stop() {
	eal.CallMain(func() {
		task.w.RemoveTask(eal.MainReadSide, task.ts)
		task.ts.closeFd()
		close(task.stopping)
		taskContextLock.Lock()
		defer taskContextLock.Unlock()
		delete(taskContextByID, task.id)
	})
}

// Finished determines if all segments have been fetched.
func (task *TaskContext) Finished() bool {
	return task.ts.Logic().Finished()
}

// TaskDef defines a fetch task that retrieves one segmented object.
type TaskDef struct {
	// InterestTemplateConfig contains the name prefix, InterestLifetime, etc.
	//
	// The fetcher neither retrieves metadata nor performs version discovery.
	// If the content is published with version component, it should appear in the name prefix.
	//
	// CanBePrefix and MustBeFresh are not normally used, but they may be included for benchmarking purpose.
	ndni.InterestTemplateConfig

	// SegmentRange specifies range of segment numbers.
	segmented.SegmentRange

	// Filename is the output file name.
	// If omitted, payload is not written to a file.
	Filename string `json:"filename,omitempty"`

	// SegmentLen is the payload length in each segment.
	// This is only needed when writing to a file.
	// If any segment has incorrect Content TLV-LENGTH, the output file would not contain correct payload.
	SegmentLen int `json:"segmentLen,omitempty"`
}

// TaskSlotConfig contains task slot configuration.
type TaskSlotConfig struct {
	// RxQueue configures the RX queue of Data packets going to each task slot.
	// CoDel cannot be used in these queues.
	RxQueue iface.PktQueueConfig `json:"rxQueue,omitempty"`

	// WindowCapacity is the maximum distance between lower and upper bounds of segment numbers in an ongoing fetch logic.
	WindowCapacity int `json:"windowCapacity,omitempty"`
}

func (cfg *TaskSlotConfig) applyDefaults() {
	cfg.RxQueue.DisableCoDel = true
	cfg.WindowCapacity = ringbuffer.AlignCapacity(cfg.WindowCapacity, 16, 65536)
}

type taskSlot C.FetchTask

// Init (re-)initializes the task slot to perform a fetch task.
// This should only be called on an inactive task slot.
func (ts *taskSlot) Init(d TaskDef) error {
	fl := ts.Logic()
	fl.Reset(d.SegmentRange)

	tpl := ndni.InterestTemplateFromPtr(unsafe.Pointer(&ts.tpl))
	d.InterestTemplateConfig.Apply(tpl)

	// FetchTask_DecodeData expects SegmentNameComponent TLV-TYPE at prefixV[prefixL]
	if uintptr(ts.tpl.prefixL+1) >= unsafe.Sizeof(ts.tpl.prefixV) {
		return errors.New("name too long")
	}
	ts.tpl.prefixV[ts.tpl.prefixL] = an.TtSegmentNameComponent

	if d.Filename != "" {
		if d.SegmentLen <= 0 || d.SegmentLen > math.MaxUint32 {
			return errors.New("bad SegmentLen")
		}
		if d.SegmentEnd <= d.SegmentBegin || d.SegmentEnd > math.MaxUint32 {
			return errors.New("bad SegmentEnd")
		}

		fd, e := unix.Open(d.Filename, unix.O_WRONLY|unix.O_CREAT, 0o666)
		if e != nil {
			return fmt.Errorf("unix.Open(%s): %w", d.Filename, e)
		}

		offsetBegin := int64(d.SegmentBegin) * int64(d.SegmentLen)
		offsetEnd := int64(d.SegmentEnd) * int64(d.SegmentLen)
		if e := unix.Fallocate(fd, 0, offsetBegin, offsetEnd-offsetBegin); e != nil {
			unix.Close(fd)
			unix.Unlink(d.Filename)
			return fmt.Errorf("unix.Fallocate(%s): %w", d.Filename, e)
		}

		ts.fd, ts.segmentLen = C.int(fd), C.uint32_t(d.SegmentLen)
	}

	logger.Info("task init",
		zap.Int("slot-index", int(ts.index)),
		zap.Stringer("prefix", d.Prefix),
		zap.Uint64s("segment-range", []uint64{d.SegmentBegin, d.SegmentEnd}),
		zap.String("filename", d.Filename),
		zap.Int("fd", int(ts.fd)),
		zap.Int("segment-len", d.SegmentLen),
	)
	return nil
}

// RxQueueD returns the RX queue for Data packets.
func (ts *taskSlot) RxQueueD() *iface.PktQueue {
	return iface.PktQueueFromPtr(unsafe.Pointer(&ts.queueD))
}

// Logic returns the congestion control and scheduling logic.
func (ts *taskSlot) Logic() *Logic {
	return (*Logic)(&ts.logic)
}

func (ts *taskSlot) closeFd() {
	if ts.fd < 0 {
		return
	}
	if e := unix.Close(int(ts.fd)); e != nil {
		logger.Warn("unix.Close error",
			zap.Int("fd", int(ts.fd)),
			zap.Error(e),
		)
	}
	ts.fd = -1
}

func newTaskSlot(index int, cfg TaskSlotConfig, socket eal.NumaSocket) (ts *taskSlot) {
	ts = eal.Zmalloc[taskSlot]("FetchTask", unsafe.Sizeof(taskSlot{}), socket)
	*ts = taskSlot{
		fd:     -1,
		index:  C.uint8_t(index),
		worker: -1,
	}
	if e := ts.RxQueueD().Init(cfg.RxQueue, socket); e != nil {
		logger.Panic("TaskSlot.RxQueueD().Init error", zap.Error(e))
	}
	ts.Logic().Init(cfg.WindowCapacity, socket)
	return
}
