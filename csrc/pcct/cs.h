#ifndef NDNDPDK_PCCT_CS_H
#define NDNDPDK_PCCT_CS_H

/** @file */

#include "cs-arc.h"
#include "pcct.h"
#include "pit-result.h"

/** @brief Constructor. */
__attribute__((nonnull)) void
Cs_Init(Cs* cs, uint32_t capDirect, uint32_t capIndirect);

/** @brief Get capacity in number of entries. */
__attribute__((nonnull)) uint32_t
Cs_GetCapacity(Cs* cs, CsListID l);

/** @brief Get number of entries. */
__attribute__((nonnull)) uint32_t
Cs_CountEntries(Cs* cs, CsListID l);

/**
 * @brief Insert a CS entry.
 * @param npkt the Data packet. CS takes ownership.
 * @param pitFound result of Pit_FindByData that contains PIT entries
 *                 satisfied by this Data; its kind must not be PIT_FIND_NONE.
 * @post PIT entries contained in @p pitFound are removed.
 */
__attribute__((nonnull)) void
Cs_Insert(Cs* cs, Packet* npkt, PitFindResult pitFound);

/**
 * @brief Determine whether the CS entry matches an Interest during PIT insertion.
 * @param entry the CS entry, possibly indirect.
 * @post the CS entry is erased if it would conflict with a PIT entry for the Interest.
 */
__attribute__((nonnull)) bool
Cs_MatchInterest(Cs* cs, CsEntry* entry, Packet* interestNpkt);

/**
 * @brief Erase a CS entry.
 * @post @p entry is no longer valid.
 */
__attribute__((nonnull)) void
Cs_Erase(Cs* cs, CsEntry* entry);

#endif // NDNDPDK_PCCT_CS_H
