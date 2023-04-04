package ndnitest

/*
#include "../../csrc/ndni/data.h"
#include "../../csrc/ndni/packet.h"
*/
import "C"
import (
	"bytes"
	"crypto/rand"
	"math"
	"testing"
	"time"
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/dpdk/pktmbuf/mbuftestenv"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/usnistgov/ndn-dpdk/ndn/an"
	"github.com/usnistgov/ndn-dpdk/ndn/tlv"
	"github.com/usnistgov/ndn-dpdk/ndni"
	"github.com/usnistgov/ndn-dpdk/ndni/ndnitestenv"
	"golang.org/x/exp/slices"
)

func ctestDataParse(t *testing.T) {
	assert, require := makeAR(t)

	// minimal
	p := makePacket(`
		060C
		0703050141 // name
		16031B0100 // siginfo
		1700 // sigvalue
	`)
	defer p.Close()
	require.True(bool(C.Packet_Parse(p.npkt, C.ParseForAny)))
	require.EqualValues(ndni.PktData, C.Packet_GetType(p.npkt))
	data := C.Packet_GetDataHdr(p.npkt)
	assert.EqualValues(1, data.name.nComps)
	assert.Equal(bytesFromHex("050141"), C.GoBytes(unsafe.Pointer(data.name.value), C.int(data.name.length)))
	assert.EqualValues(0, data.freshness)
	assert.False(bool(data.isFinalBlock))

	// full
	p = makePacket(`
		0625
		07060801`, `42080130 // name
		F000 // unknown-ignored
		140E F000 180103 19020104 1A03080131 // metainfo with unknown-ignored
		1502C0C1 // content
		16031B0100 // siginfo
		1700 // sigvalue
	`)
	defer p.Close()
	require.True(bool(C.Packet_ParseL3(p.npkt, C.ParseForAny)))
	require.EqualValues(ndni.PktData, C.Packet_GetType(p.npkt))
	data = C.Packet_GetDataHdr(p.npkt)
	assert.EqualValues(2, data.name.nComps)
	assert.Equal(bytesFromHex("080142080130"), C.GoBytes(unsafe.Pointer(data.name.value), C.int(data.name.length))) // linearized
	assert.EqualValues(260, data.freshness)
	assert.False(bool(data.isFinalBlock))

	// isFinalBlock
	p = makePacket(`
		0616
		0706080143080137 // name
		14051A0308`, `0137 // metainfo with finalblock
		16031B0100 // siginfo
		1700 // sigvalue
	`)
	defer p.Close()
	require.True(bool(C.Packet_ParseL3(p.npkt, C.ParseForAny)))
	require.EqualValues(ndni.PktData, C.Packet_GetType(p.npkt))
	data = C.Packet_GetDataHdr(p.npkt)
	assert.True(bool(data.isFinalBlock))

	// invalid: unknown-critical
	p = makePacket(`
		060E
		0703080141 // name
		F100 // unknown-critical
		16031B0100 // siginfo
		1700 // sigvalue
	`)
	defer p.Close()
	assert.False(bool(C.Packet_ParseL3(p.npkt, C.ParseForAny)))

	// invalid: MetaInfo with unknown-critical
	p = makePacket(`
		0613
		0703080141 // name
		1405 F100 180103 // metainfo with unknown-critical
		16031B0100 // siginfo
		1700 // sigvalue
	`)
	defer p.Close()
	assert.False(bool(C.Packet_ParseL3(p.npkt, C.ParseForAny)))
}

func ctestDataEncPayloadMinimal(t *testing.T) {
	assert, require := makeAR(t)

	var meta [16]C.uint8_t
	C.DataEnc_PrepareMetaInfo(unsafe.SliceData(meta[:]), an.ContentBlob, 0, C.LName{})

	nameP := ndni.NewPName(ndn.ParseName("/DataEnc/minimal"))
	defer nameP.Free()

	m := makePacket(mbuftestenv.Headroom(256))
	defer m.Close()
	npkt := C.DataEnc_EncodePayload(*(*C.LName)(nameP.Ptr()), C.LName{}, unsafe.SliceData(meta[:]), m.mbuf)
	assert.Equal(m.npkt, npkt)

	data := ndni.PacketFromPtr(m.Ptr()).ToNPacket().Data
	require.NotNil(data)
	nameEqual(assert, "/DataEnc/minimal", data)
	assert.EqualValues(an.ContentBlob, data.ContentType)
	assert.Equal(time.Duration(0), data.Freshness)
	assert.False(data.FinalBlock.Valid())
	assert.Len(data.Content, 0)
	assert.EqualValues(an.SigNull, data.SigInfo.Type)
}

func ctestDataEncPayloadFull(t *testing.T) {
	assert, require := makeAR(t)

	var meta [24]C.uint8_t
	finalBlock := ndn.NameComponentFrom(an.TtSegmentNameComponent, tlv.NNI(math.MaxUint32+1))
	finalBlockP := ndni.NewPName(ndn.Name{finalBlock})
	defer finalBlockP.Free()
	C.DataEnc_PrepareMetaInfo(unsafe.SliceData(meta[:]), an.ContentKey, 3600_000, *(*C.LName)(finalBlockP.Ptr()))

	nameP := ndni.NewPName(ndn.ParseName("/DataEnc/full"))
	defer nameP.Free()
	content := make([]byte, 512)
	rand.Read(content)

	m := makePacket(mbuftestenv.Headroom(256), content)
	defer m.Close()
	npkt := C.DataEnc_EncodePayload(*(*C.LName)(nameP.Ptr()), *(*C.LName)(finalBlockP.Ptr()), unsafe.SliceData(meta[:]), m.mbuf)
	assert.Equal(m.npkt, npkt)

	data := ndni.PacketFromPtr(m.Ptr()).ToNPacket().Data
	require.NotNil(data)
	nameEqual(assert, "/DataEnc/full/50=%00%00%00%01%00%00%00%00", data)
	assert.EqualValues(an.ContentKey, data.ContentType)
	assert.Equal(time.Hour, data.Freshness)
	assert.Equal(finalBlock, data.FinalBlock)
	assert.Equal(content, data.Content)
}

func ctestDataEnc(t *testing.T) {
	assert, _ := makeAR(t)
	mp := ndnitestenv.MakeMempools()
	mpC := (*C.PacketMempools)(unsafe.Pointer(mp))

	var metaBuf [16]C.uint8_t
	meta := unsafe.SliceData(metaBuf[:])
	C.DataEnc_PrepareMetaInfo(meta, an.ContentBlob, 0, C.LName{})
	assert.EqualValues(2, C.DataEnc_SizeofMetaInfo(meta))

	content0 := bytes.Repeat([]byte{0xC0}, 500)
	content1 := bytes.Repeat([]byte{0xC1}, 500)
	content := bytes.Join([][]byte{content0, content1}, nil)
	tpl := makePacket(content0, content1)
	var tplIov [ndni.LpMaxFragments]C.struct_iovec
	tplIovcnt := C.Mbuf_AsIovec(tpl.mbuf, unsafe.SliceData(tplIov[:]))
	assert.EqualValues(2, tplIovcnt)

	namePrefix := ndn.ParseName("/DataEnc/name/prefix")
	namePrefixP := ndni.NewPName(namePrefix)
	defer namePrefixP.Free()
	namePrefixL := *(*C.LName)(namePrefixP.Ptr())
	nameSuffix := ndn.ParseName("/suffix")
	nameSuffixP := ndni.NewPName(nameSuffix)
	defer nameSuffixP.Free()
	nameSuffixL := *(*C.LName)(nameSuffixP.Ptr())
	name := append(slices.Clone(namePrefix), nameSuffix...)

	fillContent := func(iov []C.struct_iovec, iovcnt C.size_t) {
		C.spdk_copy_buf_to_iovs(unsafe.SliceData(iov), C.int(iovcnt),
			unsafe.Pointer(unsafe.SliceData(content)), C.size_t(len(content)))
	}

	checkData := func(t *testing.T, pkt *packet) {
		assert, require := makeAR(t)
		data := pkt.N.ToNPacket().Data
		require.NotNil(data)
		nameEqual(assert, name, data)
		assert.EqualValues(an.ContentBlob, data.ContentType)
		assert.Equal(time.Duration(0), data.Freshness)
		assert.False(data.FinalBlock.Valid())
		assert.Equal(content, data.Content)
		assert.EqualValues(an.SigNull, data.SigInfo.Type)
	}

	t.Run("TplLinear", func(t *testing.T) {
		assert, require := makeAR(t)
		align := C.PacketTxAlign{
			linearize:           true,
			fragmentPayloadSize: 700,
		}

		m := C.DataEnc_EncodeTpl(namePrefixL, nameSuffixL, meta,
			tpl.mbuf, unsafe.SliceData(tplIov[:]), tplIovcnt, mpC, align)
		require.NotNil(m)

		pkt := toPacket(unsafe.Pointer(C.DataEnc_Sign(m, mpC, align)))
		require.NotNil(pkt)
		defer pkt.Close()

		checkData(t, pkt)

		if segs := pkt.SegmentBytes(); assert.Len(segs, 2) {
			assert.LessOrEqual(len(segs[0]), 700)
			assert.LessOrEqual(len(segs[1]), 700)
		}
	})

	t.Run("TplChained", func(t *testing.T) {
		assert, require := makeAR(t)
		align := C.PacketTxAlign{
			linearize: false,
		}

		m := C.DataEnc_EncodeTpl(namePrefixL, nameSuffixL, meta,
			tpl.mbuf, unsafe.SliceData(tplIov[:]), tplIovcnt, mpC, align)
		require.NotNil(m)

		pkt := toPacket(unsafe.Pointer(C.DataEnc_Sign(m, mpC, align)))
		require.NotNil(pkt)
		defer pkt.Close()

		checkData(t, pkt)
		if segs := pkt.SegmentBytes(); assert.Len(segs, 4) {
			assert.Equal(content0, segs[1])
			assert.Equal(content1, segs[2])
			assert.Len(segs[3], ndni.DataEncNullSigLen)
		}
	})

	t.Run("RoomLinear", func(t *testing.T) {
		assert, require := makeAR(t)
		align := C.PacketTxAlign{
			linearize:           true,
			fragmentPayloadSize: 700,
		}

		var roomIov [ndni.LpMaxFragments]C.struct_iovec
		var roomIovcnt C.size_t
		m := C.DataEnc_EncodeRoom(namePrefixL, nameSuffixL, meta,
			C.uint32_t(len(content)), unsafe.SliceData(roomIov[:]), &roomIovcnt, mpC, align)
		require.NotNil(m)

		fillContent(roomIov[:], roomIovcnt)

		pkt := toPacket(unsafe.Pointer(C.DataEnc_Sign(m, mpC, align)))
		require.NotNil(pkt)
		defer pkt.Close()

		checkData(t, pkt)
		if segs := pkt.SegmentBytes(); assert.Len(segs, 2) {
			assert.LessOrEqual(len(segs[0]), 700)
			assert.LessOrEqual(len(segs[1]), 700)
		}
	})

	t.Run("RoomChained", func(t *testing.T) {
		assert, require := makeAR(t)
		align := C.PacketTxAlign{
			linearize: false,
		}

		var roomIov [ndni.LpMaxFragments]C.struct_iovec
		var roomIovcnt C.size_t
		m := C.DataEnc_EncodeRoom(namePrefixL, nameSuffixL, meta,
			C.uint32_t(len(content)), unsafe.SliceData(roomIov[:]), &roomIovcnt, mpC, align)
		require.NotNil(m)

		fillContent(roomIov[:], roomIovcnt)
		pkt := toPacket(unsafe.Pointer(C.DataEnc_Sign(m, mpC, align)))
		require.NotNil(pkt)
		defer pkt.Close()

		checkData(t, pkt)
		segs := pkt.SegmentBytes()
		assert.Len(segs, 1)
	})
}
