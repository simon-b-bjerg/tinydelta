/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* Relocation by rule table (see tinydelta_reloc.h, README.md "Relocation"
 * and docs/relocation-rules.md). Which bytes are addresses depends only on
 * the raw old image near each position, the map and the table, so any span
 * of the image relocates the same whichever order the decoder reads it in.
 * The engine matches the Go encoder's (rules.go) bit for bit. Every table,
 * built in or shipped, is checked against the limits before use, and every
 * rule is read with bounds checks, so a table can't make the engine read
 * or write outside its buffers. */
#include <limits.h>

#include "tinydelta_internal.h"
#include "tinydelta_reloc.h"

#if TINYDELTA_CODER && (TINYDELTA_RELOC != 0)

/* The built-in table: a header made by "tinydelta rules compile -c", which
 * defines tinydelta_reloc_rules, TINYDELTA_RELOC_RULES_SIZE and
 * TINYDELTA_RELOC_RULES_CRC. */
#if TINYDELTA_RELOC == TINYDELTA_RELOC_THUMB
#include "tinydelta_rules_thumb.h"
#elif TINYDELTA_RELOC == TINYDELTA_RELOC_MSP430X
#include "tinydelta_rules_msp430x.h"
#elif TINYDELTA_RELOC == TINYDELTA_RELOC_RULES
#include TINYDELTA_RELOC_RULES_HEADER
#else
#error "TINYDELTA_RELOC: TINYDELTA_RELOC_THUMB, TINYDELTA_RELOC_MSP430X or TINYDELTA_RELOC_RULES"
#endif

#if TINYDELTA_RELOC_SHIPPED
#define MAX_SHIPPED_TABLE 4096u /* the longest table a patch may ship */
#endif

/* Table format and limits (docs/relocation-rules.md, "Binary table"). */
#define RULES_VERSION     1u
#define RULES_HEADER_SIZE 7u
#define LIST_PREFIXES     0u
#define LIST_LENGTHS      1u
#define LIST_CODE         2u
#define LIST_DATA         3u
#define MAX_RULES         64u
#define MAX_CONDS         4u
#define MAX_WORDS         8u
#define MAX_SIZE          16u
#define MAX_SLICES        6u
#define MAX_UNLESS        2u
#define MAX_UNLESS_AT     4u
#define MAX_UNLESS_OFFSET 16u
#define MAX_SYNC          16u
#define MAX_SKIP          4u
#define MAX_LENGTH        16u
#define SPACE_CODE        1u
#define SPACE_DATA        2u
#define DATA_BLOCK_LOG    8u /* data runs are in blocks of 256 bytes */

/* Table fields, bit counts and bit positions (below 256): 16 bits on MSP430,
 * a register on 32-bit cores, so they never take 32-bit arithmetic. */
#if (UINT_FAST16_MAX > UINT32_MAX) || (UINT_FAST16_MAX < UINT_MAX)
typedef uint32_t small_t; /* not wider than 32 bits (64-bit hosts), nor narrower than int (MinGW), so never promoted to int */
#else
typedef uint_fast16_t small_t;
#endif

/* The span being relocated. */
typedef struct {
    uint32_t offset;
    uint32_t length;
    uint8_t *p_out;
} span_t;

/* ---- the map -------------------------------------------------------------- */

/* Map entries are stored without alignment (the tables buffer has none):
 * start, length, shift (two's complement), each 4 bytes little-endian. */
static void store_le32(uint8_t *p_bytes, uint32_t value) {
    for (small_t byte_index = 0u; byte_index < 4u; byte_index++) {
        p_bytes[byte_index] = (uint8_t)value; /* Cast: keeps the low byte. */
        value >>= 8;
    }
}

static uint32_t load_le32(const uint8_t *p_bytes) {
    uint32_t value = 0u;

    for (small_t byte_index = 4u; byte_index > 0u; byte_index--) {
        value = (value << 8) | p_bytes[byte_index - 1u];
    }
    return value;
}

/* Reads a list from the header: a count, then per record its fields as
 * varints (gap since the previous record's end, length, and for 3 fields a
 * zigzag shift), stored at *pp_storage as 4-byte start, length[, shift].
 * TINYDELTA_ERR_VERSION if it doesn't fit the *p_size bytes left. */
static tinydelta_result_t
read_list(tinydelta_varint_fn_t varint, void *p_varint_ctx, uint8_t **pp_storage, uint32_t *p_size, small_t fields, uint32_t *p_count) {
    uint32_t end              = 0u;
    tinydelta_result_t result = varint(p_varint_ctx, p_count);

    for (uint32_t index = 0u; (result == TINYDELTA_OK) && (index < *p_count); index++) {
        uint32_t record[3] = {0u, 0u, 0u}; /* gap, length, zigzag shift as read */

        if (*p_size < (fields << 2)) {
            return TINYDELTA_ERR_VERSION; /* doesn't fit the tables buffer */
        }
        *p_size -= fields << 2;
        for (small_t field = 0u; (result == TINYDELTA_OK) && (field < fields); field++) {
            result = varint(p_varint_ctx, &record[field]);
        }
        if ((result == TINYDELTA_OK) && !range_within(record[0], record[1], 0xFFFFFFFFu - end)) {
            result = TINYDELTA_ERR_MALFORMED; /* past 32 bits */
        }
        record[0] += end;
        record[2] = zigzag_decode(record[2]);
        end       = record[0] + record[1];
        for (small_t field = 0u; field < fields; field++) {
            store_le32(*pp_storage, record[field]);
            *pp_storage += 4u;
        }
    }
    return result;
}

tinydelta_result_t tinydelta_reloc_read_header(tinydelta_reloc_t *p_reloc,
                                               uint8_t features,
                                               tinydelta_fetch_fn_t fetch,
                                               tinydelta_varint_fn_t varint,
                                               void *p_ctx,
                                               uint8_t *p_storage,
                                               uint32_t storage_size) {
    small_t rule              = ((small_t)features & TINYDELTA_CODED_RELOC_MASK) >> TINYDELTA_CODED_RELOC_SHIFT; /* Cast: widens. */
    tinydelta_result_t result = TINYDELTA_OK;

    p_reloc->b_shipped     = rule == TINYDELTA_CODED_RELOC_SHIPPED;
    p_reloc->run_count     = 0u;
    p_reloc->outside_count = 0u;
    if (rule == TINYDELTA_CODED_RELOC_BUILTIN) {
        /* The built-in table, named by its CRC: a patch made with another is refused. */
        uint8_t low  = 0u;
        uint8_t high = 0u;

        result = fetch(p_ctx, &low);
        if (result == TINYDELTA_OK) {
            result = fetch(p_ctx, &high);
        }
        if ((result == TINYDELTA_OK) && (((small_t)low | ((small_t)high << 8)) != TINYDELTA_RELOC_RULES_CRC)) { /* Cast: widens. */
            result = TINYDELTA_ERR_VERSION;                                                                     /* rules this decoder doesn't have */
        }
        p_reloc->p_rules   = tinydelta_reloc_rules;
        p_reloc->rules_len = TINYDELTA_RELOC_RULES_SIZE;
    }
#if TINYDELTA_RELOC_SHIPPED
    else if (rule == TINYDELTA_CODED_RELOC_SHIPPED) {
        result = varint(p_ctx, &p_reloc->rules_len);
        if ((result == TINYDELTA_OK) && ((p_reloc->rules_len == 0u) || (p_reloc->rules_len > MAX_SHIPPED_TABLE))) {
            result = TINYDELTA_ERR_MALFORMED;
        }
        if ((result == TINYDELTA_OK) && (p_reloc->rules_len > storage_size)) {
            result = TINYDELTA_ERR_VERSION; /* doesn't fit the tables buffer */
        }
        if (result != TINYDELTA_OK) {
            return result;
        }
        p_reloc->p_rules = p_storage; /* filled from the coded stream by tinydelta_reloc_start */
        p_storage += p_reloc->rules_len;
        storage_size -= p_reloc->rules_len;
    }
#endif
    else {
        return TINYDELTA_ERR_VERSION; /* reserved, or a shipped table this build doesn't accept */
    }
    if (result == TINYDELTA_OK) {
        result = varint(p_ctx, &p_reloc->base);
    }
    p_reloc->p_entries = p_storage;
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_ctx, &p_storage, &storage_size, 3u, &p_reloc->count);
    }
    p_reloc->p_runs = p_storage; /* data runs, in 256-byte blocks */
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_ctx, &p_storage, &storage_size, 2u, &p_reloc->run_count);
    }
    p_reloc->p_outside = p_storage; /* RAM ranges */
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_ctx, &p_storage, &storage_size, 3u, &p_reloc->outside_count);
    }
    return result;
}

/* The shift of the entry holding key among count entries; 0 if none. */
static uint32_t entry_shift(const uint8_t *p_entry, uint32_t count, uint32_t key) {
    for (uint32_t index = 0u; index < count; index++) {
        uint32_t start = load_le32(p_entry);

        if (start > key) {
            break; /* entries are in order */
        }
        if ((key - start) < load_le32(p_entry + 4u)) {
            return load_le32(p_entry + 8u);
        }
        p_entry += TINYDELTA_RELOC_ENTRY_SIZE;
    }
    return 0u;
}

static bool in_image(const tinydelta_reloc_t *p_reloc, uint32_t address) {
    return (address >= p_reloc->base) && ((address - p_reloc->base) < p_reloc->dict_len);
}

/* The shift of an address in a space: by the map's entries if it points
 * into the old image, else by the RAM ranges. */
static uint32_t address_shift(const tinydelta_reloc_t *p_reloc, uint32_t address, small_t space) {
    uint32_t shift = 0u;

    if (in_image(p_reloc, address)) {
        if (space != SPACE_DATA) {
            shift = entry_shift(p_reloc->p_entries, p_reloc->count, address - p_reloc->base);
        }
    } else if (space != SPACE_CODE) {
        shift = entry_shift(p_reloc->p_outside, p_reloc->outside_count, address);
    } else {
        /* an address outside the image, in the code space only: stays */
    }
    return shift;
}

/* Whether old-image offset h is in a data run. */
static bool in_data(const tinydelta_reloc_t *p_reloc, uint32_t h) {
    const uint8_t *p_run = p_reloc->p_runs;
    uint32_t block       = h >> DATA_BLOCK_LOG;

    for (uint32_t index = 0u; index < p_reloc->run_count; index++) {
        uint32_t start = load_le32(p_run);

        if (start > block) {
            break; /* runs are in order */
        }
        if ((block - start) < load_le32(p_run + 4u)) {
            return true;
        }
        p_run += 8u;
    }
    return false;
}

/* ---- the table ------------------------------------------------------------
 * Checked once, in full, before use (check_table); the engine then reads
 * rules in place without further checks. A rule: its length, flags (bits
 * 0-1 addresses, 2-4 conditions, 5 relative, 6 require, 7 unless follows),
 * attr (bits 0-1 space, 2-3 log2 align), and a prefix's skip, a length
 * rule's length or a code or data rule's size; then its conditions,
 * addresses, relative fields and unless entries. */

#define HEAD_SIZE     4u
#define FLAG_RELATIVE 0x20u
#define FLAG_REQUIRE  0x40u
#define FLAG_UNLESS   0x80u
#define SLICE_FIXED   0x80u

/* The bits of an n-bit field, n 1 to 32. */
static uint32_t low_mask(uint32_t bits) {
    return 0xFFFFFFFFu >> ((32u - bits) & 31u);
}

#if TINYDELTA_RELOC_SHIPPED
/* The table byte at offset at. */
static small_t tb(const tinydelta_reloc_t *p_reloc, small_t at) {
    return p_reloc->p_rules[at];
}

/* A little-endian value of a word's size at table offset at. */
static uint32_t table_word(const tinydelta_reloc_t *p_reloc, small_t at) {
    uint32_t value = 0u;

    for (small_t index = (small_t)p_reloc->word_bytes; index > 0u; index--) {
        value = (value << 8) | tb(p_reloc, at + index - 1u);
    }
    return value;
}

/* Checks the rule at pos (whose bytes end at end) of list, of count rules;
 * *p_size gets the bytes it needs in the image. Mirrors the Go encoder's
 * parseRule, accepting and rejecting the same tables. */
static bool check_rule(const tinydelta_reloc_t *p_reloc, small_t pos, small_t end, small_t list, small_t count, small_t *p_size) {
    small_t flags = tb(p_reloc, pos + 1u);
    small_t attr  = tb(p_reloc, pos + 2u);
    small_t n     = tb(p_reloc, pos + 3u);
    small_t addrs = flags & 3u;
    small_t at    = pos + HEAD_SIZE;
    small_t words = 1u; /* words the rule reads, at least one */
    small_t bits  = 0u; /* the first address's width */
    bool b_ok     = (addrs <= 2u) && (((flags >> 2) & 7u) <= MAX_CONDS) && ((attr & 3u) <= SPACE_DATA) && ((attr & ~0x0Fu) == 0u) &&
                ((1u << ((attr >> 2) & 3u)) >= p_reloc->unit);

    if (list < LIST_CODE) {
        b_ok = b_ok && (addrs == 0u) && ((flags & (FLAG_RELATIVE | FLAG_REQUIRE | FLAG_UNLESS)) == 0u) && ((attr & 3u) == 0u) && (n >= 1u);
        if (list == LIST_PREFIXES) {
            b_ok = b_ok && (n <= MAX_SKIP) && ((n % p_reloc->unit) == 0u);
        } else {
            b_ok = b_ok && (n <= MAX_LENGTH);
        }
    }
    for (small_t index = 0u; b_ok && (index < ((flags >> 2) & 7u)); index++) {
        /* The condition must fit the rule before any of it is read. */
        b_ok = (at + (1u + (2u * (small_t)p_reloc->word_bytes))) <= end;
        if (b_ok) {
            small_t word = tb(p_reloc, at) & 0x7Fu;

            b_ok  = (word < MAX_WORDS) && ((table_word(p_reloc, at + 1u) & ~table_word(p_reloc, at + 1u + (small_t)p_reloc->word_bytes)) == 0u);
            words = ((word + 1u) > words) ? (word + 1u) : words;
        }
        at += (1u + (2u * (small_t)p_reloc->word_bytes));
    }
    for (small_t index = 0u; b_ok && (index < addrs); index++) {
        small_t slices = (at < end) ? tb(p_reloc, at) : 0u;
        small_t width  = 0u;

        b_ok = (slices >= 1u) && (slices <= MAX_SLICES) && ((at + 1u + (2u * slices)) <= end);
        at++;
        for (small_t slice = 0u; b_ok && (slice < slices); slice++) {
            small_t x = tb(p_reloc, at);
            small_t y = tb(p_reloc, at + 1u);

            if ((y & SLICE_FIXED) != 0u) {
                b_ok = ((y & 0x7Fu) < 8u) && ((x >> ((y & 0x7Fu) + 1u)) == 0u);
                width += (y & 0x7Fu) + 1u;
            } else {
                b_ok  = (y <= 31u) && (((x & 0x1Fu) + y) < (8u * (small_t)p_reloc->word_bytes));
                words = (((x >> 5) + 1u) > words) ? ((x >> 5) + 1u) : words;
                width += y + 1u;
            }
            at += 2u;
        }
        b_ok = b_ok && (width <= 32u);
        bits = (index == 0u) ? width : bits;
    }
    if (b_ok && ((flags & FLAG_RELATIVE) != 0u)) {
        small_t sign_ext = ((at + 1u) < end) ? tb(p_reloc, at + 1u) : 0u;

        b_ok = (addrs == 1u) && (sign_ext >= 1u) && (sign_ext <= 31u) && (sign_ext <= bits);
        at += 2u;
    }
    b_ok = b_ok && !(((flags & FLAG_REQUIRE) != 0u) && ((addrs == 0u) || ((flags & FLAG_RELATIVE) != 0u)));
    if (b_ok && ((flags & FLAG_UNLESS) != 0u)) {
        small_t entries = (at < end) ? tb(p_reloc, at) : 0u;

        b_ok = (entries >= 1u) && (entries <= MAX_UNLESS);
        at++;
        for (small_t entry = 0u; b_ok && (entry < entries); entry++) {
            small_t offsets = ((at + 1u) < end) ? tb(p_reloc, at + 1u) : 0u;

            b_ok = ((at + 1u) < end) && (tb(p_reloc, at) < count) && (offsets >= 1u) && (offsets <= MAX_UNLESS_AT) && ((at + 2u + offsets) <= end);
            at += 2u;
            for (small_t index = 0u; b_ok && (index < offsets); index++) {
                small_t byte = tb(p_reloc, at);

                /* -16 to 16, a multiple of the unit (two's complement keeps a negative one's remainder) */
                b_ok = ((byte <= MAX_UNLESS_OFFSET) || (byte >= (256u - MAX_UNLESS_OFFSET))) && ((byte % p_reloc->unit) == 0u);
                at++;
            }
        }
    }
    *p_size = (list < LIST_CODE) ? (words * (small_t)p_reloc->word_bytes) : n;
    return b_ok && (at == end) && (*p_size >= (words * p_reloc->word_bytes)) && (*p_size <= MAX_SIZE);
}

/* Checks a table (a shipped one could be anything) and reads its header
 * and where its lists start. A built-in table needs no checks: "tinydelta
 * rules compile" only makes valid ones, and gives its parameters as
 * constants. */
static bool check_table(tinydelta_reloc_t *p_reloc) {
    small_t pos = RULES_HEADER_SIZE;
    small_t mode;
    bool b_ok = (p_reloc->rules_len >= RULES_HEADER_SIZE) && (p_reloc->p_rules[0] == RULES_VERSION);

    mode                = (b_ok) ? p_reloc->p_rules[1] : 3u;
    p_reloc->unit       = 1u << (mode & 3u);
    p_reloc->word_bytes = ((mode >> 2) & 3u) + 1u;
    p_reloc->b_big      = (mode & 0x10u) != 0u;
    p_reloc->sync       = (b_ok) ? p_reloc->p_rules[2] : 0u;
    p_reloc->reach      = 0u;
    b_ok                = b_ok && ((mode & ~0x1Fu) == 0u) && ((mode & 3u) != 3u) && (p_reloc->sync <= MAX_SYNC);
    for (small_t list = 0u; b_ok && (list < TINYDELTA_RULE_LISTS); list++) {
        p_reloc->list_count[list] = p_reloc->p_rules[3u + list];
        p_reloc->list_start[list] = pos;
        b_ok                      = p_reloc->list_count[list] <= MAX_RULES;
        for (small_t index = 0u; b_ok && (index < p_reloc->list_count[list]); index++) {
            small_t size = 0u;
            small_t len  = (pos < p_reloc->rules_len) ? p_reloc->p_rules[pos] : 0u;

            b_ok = (len >= HEAD_SIZE) && (len <= (p_reloc->rules_len - pos)) &&
                   check_rule(p_reloc, pos, pos + len, list, (small_t)p_reloc->list_count[list], &size);
            if (b_ok && ((size - 1u) > p_reloc->reach)) {
                p_reloc->reach = size - 1u;
            }
            pos += len;
        }
    }
    return b_ok && (pos == p_reloc->rules_len);
}
#endif

tinydelta_result_t tinydelta_reloc_start(tinydelta_reloc_t *p_reloc, tinydelta_coder_t *p_coder) {
#if TINYDELTA_RELOC_SHIPPED
    if (p_reloc->b_shipped) {
        /* The storage's start, right after the coder's tables. */
        tinydelta_result_t result = tinydelta_coder_tree_bytes(p_coder, p_coder->p_states + tinydelta_coder_tables_size(p_coder->layout), p_reloc->rules_len);

        if (result != TINYDELTA_OK) {
            return result;
        }
    }
    return check_table(p_reloc) ? TINYDELTA_OK : TINYDELTA_ERR_MALFORMED;
#else
    (void)p_reloc;
    (void)p_coder;
    return TINYDELTA_OK; /* the built-in table */
#endif
}

/* ---- the engine -----------------------------------------------------------
 * The table's parameters: from the table when patches may ship their own,
 * else the built-in table's, known at compile time, which lets the
 * compiler fold the generic word and position handling. */

#if TINYDELTA_RELOC_SHIPPED
static small_t unit_of(const tinydelta_reloc_t *p_reloc) {
    return (small_t)p_reloc->unit;
}
static small_t word_bytes_of(const tinydelta_reloc_t *p_reloc) {
    return (small_t)p_reloc->word_bytes;
}
static bool big_of(const tinydelta_reloc_t *p_reloc) {
    return p_reloc->b_big;
}
static small_t sync_of(const tinydelta_reloc_t *p_reloc) {
    return (small_t)p_reloc->sync;
}
static small_t reach_of(const tinydelta_reloc_t *p_reloc) {
    return (small_t)p_reloc->reach;
}
static small_t list_start_of(const tinydelta_reloc_t *p_reloc, small_t list) {
    return (small_t)p_reloc->list_start[list];
}
static small_t list_count_of(const tinydelta_reloc_t *p_reloc, small_t list) {
    return (small_t)p_reloc->list_count[list];
}
static bool uses(small_t feature) {
    (void)feature;
    return true; /* any table may come */
}
#else
static small_t unit_of(const tinydelta_reloc_t *p_reloc) {
    (void)p_reloc;
    return TINYDELTA_RELOC_RULES_UNIT;
}
static small_t word_bytes_of(const tinydelta_reloc_t *p_reloc) {
    (void)p_reloc;
    return TINYDELTA_RELOC_RULES_WORD;
}
static bool big_of(const tinydelta_reloc_t *p_reloc) {
    (void)p_reloc;
    return TINYDELTA_RELOC_RULES_BIG != 0;
}
static small_t sync_of(const tinydelta_reloc_t *p_reloc) {
    (void)p_reloc;
    return TINYDELTA_RELOC_RULES_SYNC;
}
static small_t reach_of(const tinydelta_reloc_t *p_reloc) {
    (void)p_reloc;
    return TINYDELTA_RELOC_RULES_REACH;
}
static small_t list_start_of(const tinydelta_reloc_t *p_reloc, small_t list) {
    static const uint16_t list_starts[TINYDELTA_RULE_LISTS] = TINYDELTA_RELOC_RULES_STARTS;

    (void)p_reloc;
    return list_starts[list];
}
static small_t list_count_of(const tinydelta_reloc_t *p_reloc, small_t list) {
    static const uint8_t counts[TINYDELTA_RULE_LISTS] = TINYDELTA_RELOC_RULES_COUNTS;

    (void)p_reloc;
    return counts[list];
}
/* Whether the built-in table uses a feature (FEATURE_...): code for the
 * others folds away. */
static bool uses(small_t feature) {
    return (TINYDELTA_RELOC_RULES_FEATURES & feature) != 0u;
}
#endif

/* Bytes of one condition. */
static small_t cond_bytes(const tinydelta_reloc_t *p_reloc) {
    return 1u + (2u * word_bytes_of(p_reloc));
}

/* Features a table may use (TINYDELTA_RELOC_RULES_FEATURES). */
#define FEATURE_RELATIVE 1u
#define FEATURE_UNLESS   2u
#define FEATURE_REQUIRE  4u
#define FEATURE_FIXED    8u

/* A rule's flags, attr and size bytes. */
#define RULE_FLAGS(p_rule) ((small_t)(p_rule)[1])
#define RULE_ATTR(p_rule)  ((small_t)(p_rule)[2])
#define RULE_N(p_rule)     ((small_t)(p_rule)[3])

static uint8_t raw_byte(const tinydelta_reloc_t *p_reloc, uint32_t index) {
    return (p_reloc->p_dict != NULL) ? p_reloc->p_dict[index] : p_reloc->raw(p_reloc->p_raw_ctx, index);
}

/* Word w of a rule at position p (the caller checked the bounds). */
static uint32_t word_at(const tinydelta_reloc_t *p_reloc, uint32_t p, small_t w) {
    uint32_t at    = p + (w * word_bytes_of(p_reloc));
    uint32_t value = 0u;

    for (small_t index = 0u; index < word_bytes_of(p_reloc); index++) {
        value = (value << 8) | raw_byte(p_reloc, at + (big_of(p_reloc) ? index : (word_bytes_of(p_reloc) - 1u - index)));
    }
    return value;
}

/* A little-endian value of a word's size in the table. */
static uint32_t rule_word(const tinydelta_reloc_t *p_reloc, const uint8_t *p_bytes) {
    uint32_t value = 0u;

    for (small_t index = word_bytes_of(p_reloc); index > 0u; index--) {
        value = (value << 8) | p_bytes[index - 1u];
    }
    return value;
}

/* Address k of a rule (k = its address count: just past its addresses). */
static const uint8_t *rule_addr(const tinydelta_reloc_t *p_reloc, const uint8_t *p_rule, small_t k) {
    const uint8_t *p_at = p_rule + HEAD_SIZE;

    for (small_t index = (RULE_FLAGS(p_rule) >> 2) & 7u; index > 0u; index--) {
        p_at += cond_bytes(p_reloc); /* past the conditions */
    }
    for (small_t index = 0u; index < k; index++) {
        p_at = &p_at[1u + (small_t)*p_at + (small_t)*p_at]; /* Cast: widens. Past its slice count and two bytes per slice. */
    }
    return p_at;
}

/* Whether n bytes from p are inside the image. */
static bool inside(const tinydelta_reloc_t *p_reloc, uint32_t p, small_t n) {
    return (n <= p_reloc->dict_len) && (p <= (p_reloc->dict_len - n));
}

/* Whether a rule is aligned at p and its conditions hold there, the words
 * they read inside the image. */
static bool matches(const tinydelta_reloc_t *p_reloc, const uint8_t *p_rule, uint32_t p) {
    const uint8_t *p_cond = p_rule + HEAD_SIZE;
    bool b_ok             = ((p_reloc->base + p) & ((1u << ((RULE_ATTR(p_rule) >> 2) & 3u)) - 1u)) == 0u;

    for (small_t index = (RULE_FLAGS(p_rule) >> 2) & 7u; b_ok && (index > 0u); index--) {
        small_t word = (small_t)*p_cond & 0x7Fu; /* Cast: widens. */

        b_ok = inside(p_reloc, p, (word + 1u) * word_bytes_of(p_reloc)) &&
               (((word_at(p_reloc, p, word) & rule_word(p_reloc, &p_cond[1u + word_bytes_of(p_reloc)])) == rule_word(p_reloc, &p_cond[1])) !=
                ((*p_cond & 0x80u) != 0u));
        p_cond += cond_bytes(p_reloc);
    }
    return b_ok;
}

/* The byte of the image holding bit `bit` of word w of a rule at p (bits
 * counted from the word's least significant). */
static uint32_t bit_byte(const tinydelta_reloc_t *p_reloc, uint32_t p, small_t w, small_t bit) {
    small_t index = bit >> 3;

    return p + (w * word_bytes_of(p_reloc)) + (big_of(p_reloc) ? ((word_bytes_of(p_reloc) - 1u) - index) : index);
}

/* An address's value at p (p_addr: its slices, most significant first),
 * gathered bit by bit (shifts by one only: cheap on 16-bit cores); its
 * width in *p_bits. */
static uint32_t join(const tinydelta_reloc_t *p_reloc, const uint8_t *p_addr, uint32_t p, small_t *p_bits) {
    uint32_t value = 0u;

    *p_bits = 0u;
    for (small_t index = *p_addr; index > 0u; index--) {
        small_t x = p_addr[1];
        small_t y = p_addr[2];

        for (small_t bit = (y & 0x7Fu) + 1u; bit > 0u; bit--) { /* from the slice's most significant bit */
            small_t one = (x >> (bit - 1u)) & 1u;               /* fixed bits: their value */

            if ((y & SLICE_FIXED) == 0u) {
                small_t at = (x & 0x1Fu) + (bit - 1u);

                one = ((small_t)raw_byte(p_reloc, bit_byte(p_reloc, p, x >> 5, at)) >> (at & 7u)) & 1u; /* Cast: widens. */
            }
            value = (value << 1) | one;
        }
        *p_bits += (y & 0x7Fu) + 1u;
        p_addr += 2u;
    }
    return value;
}

/* Writes value into an address's slices at p, the bytes of them inside the
 * span; nothing if it doesn't fit their fixed bits. Bit by bit, from the
 * least significant: first a pass that only checks the fixed bits. */
static void split(const tinydelta_reloc_t *p_reloc, const span_t *p_span, const uint8_t *p_addr, uint32_t p, uint32_t value) {
    bool b_fits = true;

    for (small_t pass = uses(FEATURE_FIXED) ? 0u : 1u; b_fits && (pass < 2u); pass++) {
        uint32_t rest          = value;
        const uint8_t *p_slice = &p_addr[1u + (small_t)*p_addr + (small_t)*p_addr]; /* Cast: widens. Just past the last slice. */

        for (small_t index = *p_addr; b_fits && (index > 0u); index--) {
            small_t x;
            small_t y;

            p_slice -= 2u;
            x = p_slice[0];
            y = p_slice[1];
            for (small_t bit = 0u; bit <= (y & 0x7Fu); bit++) {
                if ((y & SLICE_FIXED) != 0u) {
                    b_fits = b_fits && ((rest & 1u) == ((x >> bit) & 1u));
                } else if (pass == 1u) {
                    small_t at        = (x & 0x1Fu) + bit;
                    uint32_t position = bit_byte(p_reloc, p, x >> 5, at) - p_span->offset; /* wraps below the span */

                    if (position < p_span->length) {
                        small_t mask = (small_t)1u << (at & 7u); /* a byte's bit */

                        /* Cast: keeps the byte. Only the slice's bits change. */
                        p_span->p_out[position] = (uint8_t)(((rest & 1u) != 0u) ? (p_span->p_out[position] | mask) : (p_span->p_out[position] & ~mask));
                    }
                } else {
                    /* checking only */
                }
                rest >>= 1;
            }
        }
    }
}

/* The first rule of a list matching at p (NULL if none), its index in
 * *p_index. A prefix or length rule must fit the image; one with require
 * counts only if it fits and its first address points into the image. */
static const uint8_t *first_rule(const tinydelta_reloc_t *p_reloc, small_t list, uint32_t p, small_t *p_index) {
    const uint8_t *p_rule = p_reloc->p_rules + list_start_of(p_reloc, list);

    for (small_t index = 0u; index < list_count_of(p_reloc, list); index++) {
        small_t bits;

        if (matches(p_reloc, p_rule, p) && ((list >= LIST_CODE) || inside(p_reloc, p, word_bytes_of(p_reloc))) &&
            (!uses(FEATURE_REQUIRE) || ((RULE_FLAGS(p_rule) & FLAG_REQUIRE) == 0u) ||
             (inside(p_reloc, p, RULE_N(p_rule)) && in_image(p_reloc, join(p_reloc, rule_addr(p_reloc, p_rule, 0u), p, &bits))))) {
            *p_index = index;
            return p_rule;
        }
        p_rule += *p_rule;
    }
    return NULL;
}

/* The instruction length at q for finding instruction starts: 0 outside
 * the image, at a misaligned position or in a data run. */
static small_t length_at(const tinydelta_reloc_t *p_reloc, uint32_t q) {
    small_t index;
    const uint8_t *p_rule;
    small_t skip = 0u;

    if (!inside(p_reloc, q, word_bytes_of(p_reloc)) || (((p_reloc->base + q) & (unit_of(p_reloc) - 1u)) != 0u) || in_data(p_reloc, q)) {
        return 0u;
    }
    p_rule = first_rule(p_reloc, LIST_PREFIXES, q, &index);
    if ((p_rule != NULL) && inside(p_reloc, q, RULE_N(p_rule) + word_bytes_of(p_reloc))) {
        skip = RULE_N(p_rule);
    }
    p_rule = first_rule(p_reloc, LIST_LENGTHS, q + skip, &index);
    return skip + ((p_rule != NULL) ? RULE_N(p_rule) : unit_of(p_reloc));
}

/* Whether p starts an instruction: decoding lengths from sync units before
 * lands on it. */
static bool starts(const tinydelta_reloc_t *p_reloc, uint32_t p) {
    uint32_t covered = 0u; /* bit i: the unit i units on is inside an instruction */

    for (small_t i = sync_of(p_reloc); i > 0u; i--) {
        if (((covered & 1u) == 0u) && ((i * unit_of(p_reloc)) <= p)) {
            small_t units = length_at(p_reloc, p - (i * unit_of(p_reloc))) / unit_of(p_reloc);

            if (units > 0u) {
                covered |= low_mask(units) & ~1u;
            }
        }
        covered >>= 1;
    }
    return (covered & 1u) == 0u;
}

/* The rule that applies at p before unless (NULL if none, or an
 * exclusion), its list and index. */
static const uint8_t *applies(const tinydelta_reloc_t *p_reloc, uint32_t p, small_t *p_list, small_t *p_index) {
    const uint8_t *p_rule = NULL;

    *p_list = in_data(p_reloc, p) ? LIST_DATA : LIST_CODE;
    if ((p < p_reloc->dict_len) && (((p_reloc->base + p) & (unit_of(p_reloc) - 1u)) == 0u)) {
        p_rule = first_rule(p_reloc, *p_list, p, p_index);
    }
    if ((p_rule != NULL) && (((RULE_FLAGS(p_rule) & 3u) == 0u) || !inside(p_reloc, p, RULE_N(p_rule)) ||
                             ((*p_list == LIST_CODE) && (sync_of(p_reloc) > 0u) && !starts(p_reloc, p)))) {
        p_rule = NULL;
    }
    return p_rule;
}

/* Whether a rule, which applies at p in list, is excluded by its unless
 * entries: their rule applies at one of their offsets. */
static bool excluded(const tinydelta_reloc_t *p_reloc, const uint8_t *p_rule, small_t list, uint32_t p) {
    const uint8_t *p_at = rule_addr(p_reloc, p_rule, RULE_FLAGS(p_rule) & 3u) + (((RULE_FLAGS(p_rule) & FLAG_RELATIVE) != 0u) ? 2u : 0u);
    bool b_excluded     = false;

    if (uses(FEATURE_UNLESS) && ((RULE_FLAGS(p_rule) & FLAG_UNLESS) != 0u)) {
        small_t entries = *p_at;

        p_at++;
        for (; entries > 0u; entries--) {
            small_t rule    = p_at[0];
            small_t offsets = p_at[1];

            p_at += 2u;
            for (; offsets > 0u; offsets--) {
                small_t other_list;
                small_t other;
                /* Two's complement, so a negative offset wraps below p (past the image if p is near 0). */
                uint32_t q = p + ((*p_at >= 0x80u) ? ((uint32_t)*p_at | 0xFFFFFF00u) : (uint32_t)*p_at); /* Cast: widens. */

                if ((applies(p_reloc, q, &other_list, &other) != NULL) && (other == rule) && (other_list == list)) {
                    b_excluded = true;
                }
                p_at++;
            }
        }
    }
    return b_excluded;
}

/* Whether a + shift (two's complement) stays within 0 to 2^32 - 1. */
static bool no_wrap(uint32_t a, uint32_t shift) {
    return ((shift & 0x80000000u) != 0u) ? ((a + shift) < a) : ((a + shift) >= a);
}

/* Rewrites a rule's addresses at p in the span. */
static void move_at(const tinydelta_reloc_t *p_reloc, const uint8_t *p_rule, uint32_t p, const span_t *p_span) {
    small_t space = RULE_ATTR(p_rule) & 3u;
    small_t bits;

    if (uses(FEATURE_RELATIVE) && ((RULE_FLAGS(p_rule) & FLAG_RELATIVE) != 0u)) {
        const uint8_t *p_addr = rule_addr(p_reloc, p_rule, 0u);
        const uint8_t *p_rel  = rule_addr(p_reloc, p_rule, 1u);
        uint32_t sign         = (uint32_t)1u << (p_rel[1] - 1u); /* Cast: unsigned int is 16 bits on MSP430 */
        /* The offset, sign-extended (two's complement), and the site's address. */
        uint32_t offset       = ((join(p_reloc, p_addr, p, &bits) & ((sign << 1) - 1u)) ^ sign) - sign;
        uint32_t site         = p_reloc->base + p;
        uint32_t delta        = offset + ((p_rel[0] >= 0x80u) ? ((uint32_t)p_rel[0] | 0xFFFFFF00u) : (uint32_t)p_rel[0]); /* Cast: widens; |.| < 2^31. */
        uint32_t next         = offset - address_shift(p_reloc, site, SPACE_CODE);

        if (no_wrap(site, delta)) {
            next += address_shift(p_reloc, site + delta, space);
        }
        /* Within sign_extend bits (as two's complement), and changed. */
        if ((next != offset) && ((next + sign) < (sign << 1))) {
            split(p_reloc, p_span, p_addr, p, next & low_mask(bits));
        }
    } else {
        const uint8_t *p_addr = rule_addr(p_reloc, p_rule, 0u);

        for (small_t index = RULE_FLAGS(p_rule) & 3u; index > 0u; index--) {
            uint32_t address = join(p_reloc, p_addr, p, &bits);
            uint32_t shift   = address_shift(p_reloc, address, space);

            /* Not negative nor past 32 bits, and within the slices' bits. */
            if ((shift != 0u) && no_wrap(address, shift) && ((bits >= 32u) || (((address + shift) >> bits) == 0u))) {
                split(p_reloc, p_span, p_addr, p, address + shift);
            }
            p_addr = &p_addr[1u + (small_t)*p_addr + (small_t)*p_addr]; /* Cast: widens. The next address. */
        }
    }
}

void tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length) {
    span_t span  = {offset, length, p_out};
    uint32_t end = offset + length;

    for (uint32_t p = (offset >= reach_of(p_reloc)) ? (offset - reach_of(p_reloc)) : 0u; p < end; p++) {
        small_t list;
        small_t index;
        const uint8_t *p_rule = applies(p_reloc, p, &list, &index);

        if ((p_rule != NULL) && !excluded(p_reloc, p_rule, list, p)) {
            move_at(p_reloc, p_rule, p, &span);
        }
    }
}

#endif
