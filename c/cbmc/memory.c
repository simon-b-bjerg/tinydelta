/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* CBMC harness: the memory decoder, for every dictionary, patch and
 * output capacity up to the bounds below (see scripts/c-cbmc.sh). Proves, within
 * the bounds: no out-of-bounds access, no undefined behaviour, one of the
 * documented results, and a target length within the capacity. */
#include <assert.h>

#include "tinydelta.h"

#ifndef DICT_MAX
#define DICT_MAX 4
#endif
#ifndef PATCH_MAX
#define PATCH_MAX 8
#endif
#ifndef OUT_MAX
#define OUT_MAX 8
#endif

uint32_t nondet_u32(void);

int main(void) {
    uint32_t dict_len = nondet_u32(), patch_len = nondet_u32(), out_cap = nondet_u32();
    __CPROVER_assume(dict_len <= DICT_MAX && patch_len <= PATCH_MAX && out_cap <= OUT_MAX);
    /* Each input ends where its array ends, so reading or writing one
     * byte past it leaves the array, which CBMC reports. */
    static uint8_t dict_area[DICT_MAX], patch_area[PATCH_MAX], out_area[OUT_MAX];
    for (uint32_t i = 0; i < DICT_MAX; i++) dict_area[i] = (uint8_t)nondet_u32();
    for (uint32_t i = 0; i < PATCH_MAX; i++) patch_area[i] = (uint8_t)nondet_u32();
    const uint8_t *dict = dict_area + (DICT_MAX - dict_len), *patch = patch_area + (PATCH_MAX - patch_len);
    uint8_t *out = out_area + (OUT_MAX - out_cap);
    uint32_t out_len = 0xFFFFFFFFu;

    tinydelta_result_t result = tinydelta_decode(dict, dict_len, patch, patch_len, out, out_cap, &out_len);

    assert(result == TINYDELTA_OK || result == TINYDELTA_ERR_MALFORMED || result == TINYDELTA_ERR_TOO_LARGE ||
           result == TINYDELTA_ERR_VERSION);
    if (result == TINYDELTA_OK) assert(out_len <= out_cap);
    return 0;
}
