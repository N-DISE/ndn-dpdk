package main

import (
	"log"
	"os"
	"time"

	"ndn-dpdk/dpdk"
	"ndn-dpdk/iface"
	"ndn-dpdk/iface/ethface"
	"ndn-dpdk/ndn"
)

// exit codes
const (
	EXIT_ARG_ERROR         = 2
	EXIT_DPDK_ERROR        = 3
	EXIT_CREATE_FACE_ERROR = 4
)

// static configuration
const (
	MP_CAPACITY  = 255
	MP_CACHE     = 0
	MP_DATAROOM  = 2000
	RXQ_CAPACITY = 64
	TXQ_CAPACITY = 64
	BURST_SIZE   = 8
)

var eal *dpdk.Eal
var mpRx dpdk.PktmbufPool
var mpTxHdr dpdk.PktmbufPool
var mpIndirect dpdk.PktmbufPool
var rxFace *iface.Face
var txFaces []*iface.Face

func main() {
	var e error
	eal, e = dpdk.NewEal(os.Args)
	if e != nil {
		log.Print("NewEal:", e)
		os.Exit(EXIT_DPDK_ERROR)
	}

	pc, e := parseCommand()

	mpRx, e = dpdk.NewPktmbufPool("MP-RX", MP_CAPACITY, MP_CACHE,
		ndn.SizeofPacketPriv(), MP_DATAROOM, dpdk.NUMA_SOCKET_ANY)
	if e != nil {
		log.Printf("NewPktmbufPool(RX): %v", e)
		os.Exit(EXIT_DPDK_ERROR)
	}

	mpTxHdr, e = dpdk.NewPktmbufPool("MP-TXHDR", MP_CAPACITY, MP_CACHE,
		0, ethface.SizeofHeaderMempoolDataRoom(), dpdk.NUMA_SOCKET_ANY)
	if e != nil {
		log.Printf("NewPktmbufPool(TXHDR): %v", e)
		os.Exit(EXIT_DPDK_ERROR)
	}

	mpIndirect, e = dpdk.NewPktmbufPool("MP-IND", MP_CAPACITY, MP_CACHE,
		0, 0, dpdk.NUMA_SOCKET_ANY)
	if e != nil {
		log.Printf("NewPktmbufPool(IND): %v", e)
		os.Exit(EXIT_DPDK_ERROR)
	}

	rxFace, _, e = createFaceFromUri(pc.inface)
	if e != nil {
		log.Printf("createFaceFromUri(%s): %v", pc.inface, e)
		os.Exit(EXIT_CREATE_FACE_ERROR)
	}

	for _, outface := range pc.outfaces {
		txFace, isNew, e := createFaceFromUri(outface)
		if e != nil {
			log.Printf("createFaceFromUri(%s): %v", outface, e)
			os.Exit(EXIT_CREATE_FACE_ERROR)
		}
		if !isNew {
			log.Printf("duplicate face %s", outface)
			os.Exit(EXIT_ARG_ERROR)
		}
		txFaces = append(txFaces, txFace)
	}

	tick := time.Tick(pc.counterInterval)
	go func() {
		for {
			<-tick
			log.Printf("RX-cnt %d %v", rxFace.GetFaceId(), rxFace.ReadCounters())
			for _, txFace := range txFaces {
				log.Printf("TX-cnt %d %v", txFace.GetFaceId(), txFace.ReadCounters())
			}
		}
	}()

	for {
		var pkts [BURST_SIZE]ndn.Packet
		nPkts := rxFace.RxBurst(pkts[:])
		if nPkts == 0 {
			continue
		}
		for _, txFace := range txFaces {
			txFace.TxBurst(pkts[:nPkts])
		}
		for _, pkt := range pkts[:nPkts] {
			if pc.wantDump {
				printPacket(pkt)
			}
			pkt.Close()
		}
	}
}

func printPacket(pkt ndn.Packet) {
	switch pkt.GetNetType() {
	case ndn.NdnPktType_Interest:
		interest := pkt.AsInterest()
		log.Printf("I %s", interest.GetName())
	case ndn.NdnPktType_Data:
		data := pkt.AsData()
		log.Printf("D %s", data.GetName())
	case ndn.NdnPktType_Nack:
		log.Printf("Nack")
	}
}
