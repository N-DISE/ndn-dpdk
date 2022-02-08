package bdev

/*
#include "../../csrc/dpdk/bdev.h"
*/
import "C"
import (
	"io"
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/core/cptr"
	"github.com/usnistgov/ndn-dpdk/dpdk/spdkenv"
	"go.uber.org/zap"
)

// IOType represents an I/O type.
type IOType int

const (
	IORead      IOType = C.SPDK_BDEV_IO_TYPE_READ
	IOWrite     IOType = C.SPDK_BDEV_IO_TYPE_WRITE
	IOUnmap     IOType = C.SPDK_BDEV_IO_TYPE_UNMAP
	IONvmeAdmin IOType = C.SPDK_BDEV_IO_TYPE_NVME_ADMIN
	IONvmeIO    IOType = C.SPDK_BDEV_IO_TYPE_NVME_IO
)

// Device interface allows retrieving bdev Info.
type Device interface {
	DevInfo() *Info
}

// DeviceCloser interface is a device that can be closed.
type DeviceCloser interface {
	Device
	io.Closer
}

// Info provides information about a block device.
type Info C.struct_spdk_bdev

var _ Device = (*Info)(nil)

func (bdi *Info) ptr() *C.struct_spdk_bdev {
	return (*C.struct_spdk_bdev)(bdi)
}

// Name returns device name.
func (bdi *Info) Name() string {
	return C.GoString(C.spdk_bdev_get_name(bdi.ptr()))
}

// ProductName returns product name.
func (bdi *Info) ProductName() string {
	return C.GoString(C.spdk_bdev_get_product_name(bdi.ptr()))
}

// BlockSize returns logical block size.
func (bdi *Info) BlockSize() int {
	return int(C.spdk_bdev_get_block_size(bdi.ptr()))
}

// CountBlocks returns size of block device in logical blocks.
func (bdi *Info) CountBlocks() int {
	return int(C.spdk_bdev_get_num_blocks(bdi.ptr()))
}

// HasIOType determines whether the I/O type is supported.
func (bdi *Info) HasIOType(ioType IOType) bool {
	return bool(C.spdk_bdev_io_type_supported(bdi.ptr(), C.enum_spdk_bdev_io_type(ioType)))
}

// DriverInfo returns driver-specific information.
func (bdi *Info) DriverInfo() (value interface{}) {
	var res C.int
	e := cptr.CaptureSpdkJSON(cptr.SpdkJSONObject(func(w unsafe.Pointer) {
		res = C.spdk_bdev_dump_info_json(bdi.ptr(), (*C.struct_spdk_json_write_ctx)(w))
	}), &value)
	if res != 0 || e != nil {
		logger.Warn("spdk_bdev_dump_info_json error",
			zap.Int("res", int(res)),
			zap.Error(e),
		)
		return nil
	}
	return value
}

// DevInfo implements Device interface.
func (bdi *Info) DevInfo() *Info {
	return bdi
}

// List returns a list of existing block devices.
func List() (list []*Info) {
	initBdevLib()
	for d := C.spdk_bdev_first(); d != nil; d = C.spdk_bdev_next(d) {
		list = append(list, (*Info)(d))
	}
	return list
}

// Find finds a block device by name.
func Find(name string) *Info {
	initBdevLib()
	nameC := C.CString(name)
	defer C.free(unsafe.Pointer(nameC))
	d := C.spdk_bdev_get_by_name(nameC)
	return (*Info)(d)
}

func mustFind(name string) *Info {
	info := Find(name)
	if info == nil {
		logger.Panic("bdev not found",
			zap.String("name", name),
		)
	}
	return info
}

func deleteByName(method, name string) error {
	args := struct {
		Name string `json:"name"`
	}{
		Name: name,
	}
	var ok bool
	return spdkenv.RPC(method, args, &ok)
}
