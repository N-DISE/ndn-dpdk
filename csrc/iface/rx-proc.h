#ifndef NDNDPDK_IFACE_RX_PROC_H
#define NDNDPDK_IFACE_RX_PROC_H

/** @file */

#include "reassembler.h"

#define RXPROC_MAX_THREADS 8

/** @brief RxProc per-thread information. */
typedef struct RxProcThread
{
  uint64_t nFrames[PktMax]; ///< accepted L3 packets; nFrames[0] is nOctets
  uint64_t nDecodeErr;      ///< decode errors
} __rte_cache_aligned RxProcThread;

/**
 * @brief Incoming frame processing procedure.
 */
typedef struct RxProc
{
  Reassembler reass;
  RxProcThread threads[RXPROC_MAX_THREADS];
} RxProc;

/**
 * @brief Process an incoming L2 frame.
 * @param pkt incoming L2 frame, starting from NDNLP header;
 *            RxProc retains ownership of this packet.
 * @return L3 packet after @c Packet_ParseL3;
 *         RxProc releases ownership of this packet.
 * @retval NULL no L3 packet is ready at this moment.
 */
__attribute__((nonnull)) Packet*
RxProc_Input(RxProc* rx, int thread, struct rte_mbuf* pkt);

#endif // NDNDPDK_IFACE_RX_PROC_H
