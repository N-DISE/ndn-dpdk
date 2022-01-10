#include "writer.h"
#include "../iface/faceid.h"
#include "format.h"

__attribute__((nonnull)) static __rte_noinline void
WriteBlock(PdumpWriter* w, struct rte_mbuf* pkt)
{
  NDNDPDK_ASSERT(pkt->pkt_len % 4 == 0);
  NDNDPDK_ASSERT(pkt->pkt_len == pkt->data_len);
  rte_memcpy(MmapFd_At(&w->m, w->pos), rte_pktmbuf_mtod(pkt, const uint8_t*), pkt->pkt_len);
  w->pos += pkt->pkt_len;
}

__attribute__((nonnull)) static inline void
WriteSLL(PdumpWriter* w, struct rte_mbuf* pkt, uint32_t totalLength)
{
  uint32_t intf = w->intf[pkt->port];
  if (unlikely(intf == UINT32_MAX)) {
    return;
  }

  uint64_t time = TscTime_ToUnixNano(Mbuf_GetTimestamp(pkt));
  rte_le32_t pktLen = SLL_HDR_LEN + pkt->pkt_len;
  PcapngEPBSLL hdr = {
    .epb = {
      .blockType = rte_cpu_to_le_32(PdumpNgTypeEPB),
      .totalLength = rte_cpu_to_le_32(totalLength),
      .intf = rte_cpu_to_le_32(intf),
      .timeHi = rte_cpu_to_le_32(time >> 32),
      .timeLo = rte_cpu_to_le_32(time & UINT32_MAX),
      .capLen = rte_cpu_to_le_32(pktLen),
      .origLen = rte_cpu_to_le_32(pktLen),
    },
    .sll = {
      .sll_pkttype = pkt->packet_type,
      .sll_hatype = rte_cpu_to_be_16(UINT16_MAX),
      .sll_protocol = rte_cpu_to_be_16(EtherTypeNDN),
    },
  };
  rte_memcpy(MmapFd_At(&w->m, w->pos), &hdr, sizeof(hdr));

  PcapngTrailer trailer = {
    .totalLength = hdr.epb.totalLength,
  };
  rte_memcpy(MmapFd_At(&w->m, w->pos + totalLength - sizeof(trailer)), &trailer, sizeof(trailer));

  uint8_t* dst = MmapFd_At(&w->m, w->pos + sizeof(hdr));
  const uint8_t* readTo = rte_pktmbuf_read(pkt, 0, pkt->pkt_len, dst);
  if (readTo != dst) {
    rte_memcpy(dst, readTo, pkt->pkt_len);
  }

  w->pos += totalLength;
}

__attribute__((nonnull)) static inline bool
ProcessMbuf(PdumpWriter* w, struct rte_mbuf* pkt)
{
  uint32_t len4 = (pkt->pkt_len + 0x03) & (~0x03);
  uint32_t totalLength = sizeof(PcapngEPBSLL) + len4 + sizeof(PcapngTrailer);
  if (w->pos + totalLength > w->m.size) {
    return true;
  }

  switch (pkt->packet_type) {
    case SLLIncoming:
    case SLLOutgoing:
      WriteSLL(w, pkt, totalLength);
      break;
    case PdumpNgTypeIDB:
      w->intf[pkt->port] = w->nextIntf++;
      // fallthrough
    case PdumpNgTypeSHB:
      WriteBlock(w, pkt);
      break;
    default:
      NDNDPDK_ASSERT(false);
      break;
  }
  return false;
}

int
PdumpWriter_Run(PdumpWriter* w)
{
  if (!MmapFd_Open(&w->m, w->filename, w->maxSize)) {
    return 1;
  }

  uint16_t count = 0;
  bool full = false;
  while (ThreadCtrl_Continue(w->ctrl, count) && !full) {
    struct rte_mbuf* pkts[PdumpWriterBurstSize];
    count = rte_ring_dequeue_burst(w->queue, (void**)pkts, RTE_DIM(pkts), NULL);

    for (uint16_t i = 0; i < count && !full; ++i) {
      full = ProcessMbuf(w, pkts[i]);
    }
    rte_pktmbuf_free_bulk(pkts, count);
  }

  if (!MmapFd_Close(&w->m, w->pos)) {
    return 2;
  }
  return 0;
}
