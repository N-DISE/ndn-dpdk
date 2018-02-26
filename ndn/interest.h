#ifndef NDN_DPDK_NDN_INTEREST_H
#define NDN_DPDK_NDN_INTEREST_H

/// \file

#include "name.h"

/** \brief maximum number of forwarding hints
 */
#define INTEREST_MAX_FHS 4

#define DEFAULT_INTEREST_LIFETIME 4000

/** \brief Interest HopLimit field.
 */
typedef uint16_t HopLimit;
enum HopLimitSpecial
{
  HOP_LIMIT_OMITTED = 0x0101, ///< HopLimit is omitted
  HOP_LIMIT_ZERO = 0x0100,    ///< HopLimit was zero before decrementing
};

/** \brief Parsed Interest packet.
 */
typedef struct PInterest
{
  Name name;
  uint32_t nonce;    ///< Nonce interpreted as little endian
  uint32_t lifetime; ///< InterestLifetime in millis
  HopLimit hopLimit; ///< HopLimit value after decrementing, or HopLimitSpecial
  bool canBePrefix;
  bool mustBeFresh;
  uint8_t nFhs;       ///< number of forwarding hints in \p fh
  int8_t thisFhIndex; ///< index of current forwarding hint in \p thisFh, or -1
  LName fh[INTEREST_MAX_FHS];
  Name thisFh; ///< a parsed forwarding hint at index \p thisFhIndex
} PInterest;

/** \brief Parse a packet as Interest.
 *  \param[out] interest the parsed Interest packet.
 *  \param pkt the packet.
 *  \param nameMp mempool for allocating Name linearize mbufs,
 *                requires at least \p NAME_MAX_LENGTH dataroom.
 *  \retval NdnError_BadType packet is not Interest.
 *  \retval NdnError_AllocError unable to allocate mbuf.
 */
NdnError PInterest_FromPacket(PInterest* interest, struct rte_mbuf* pkt,
                              struct rte_mempool* nameMp);

/** \brief Parse a forwarding hint.
 *  \param index forwarding hint index, must be less than \p interest->nFhs.
 *  \post interest->thisFhIndex == index
 *  \post interest->thisFh reflects the index-th forwarding hint.
 */
NdnError PInterest_ParseFh(PInterest* interest, uint8_t index);

#endif // NDN_DPDK_NDN_INTEREST_H
