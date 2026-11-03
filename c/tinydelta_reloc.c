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
#include "tinydelta_reloc.h"
#include "tinydelta_internal.h"

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

/* The CRCs of rules/thumb.yaml and rules/msp430x.yaml, which features
 * values 1 and 2 name (checked against the generated headers by the Go
 * tests). */
#define RULES_THUMB_CRC   0x39A2u
#define RULES_MSP430X_CRC 0xF992u

/* Features: a table named by CRC or shipped, and the shipped bit. */
#define FEATURES_RULE_TABLE 3u
#define FEATURES_SHIPPED    0x10u
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
    for (uint32_t byte_index = 0u; byte_index < 4u; byte_index++) {
        p_bytes[byte_index] = (uint8_t)value; /* Cast: keeps the low byte. */
        value >>= 8;
    }
}

static uint32_t load_le32(const uint8_t *p_bytes) {
    uint32_t value = 0u;

    for (uint32_t byte_index = 4u; byte_index > 0u; byte_index--) {
        value = (value << 8) | p_bytes[byte_index - 1u];
    }
    return value;
}

/* Reads a list from the header: a count, then per record its fields as
 * varints (gap since the previous record's end, length, and for 3 fields a
 * zigzag shift), stored at *pp_storage as 4-byte start, length[, shift].
 * TINYDELTA_ERR_VERSION if it doesn't fit the *p_size bytes left. */
static tinydelta_result_t
read_list(tinydelta_varint_fn_t varint, void *p_varint_ctx, uint8_t **pp_storage, uint32_t *p_size, uint32_t fields, uint32_t *p_count) {
    uint32_t end              = 0u;
    tinydelta_result_t result = varint(p_varint_ctx, p_count);

    for (uint32_t index = 0u; (result == TINYDELTA_OK) && (index < *p_count); index++) {
        uint32_t record[3] = {0u, 0u, 0u}; /* gap, length, zigzag shift as read */

        if (*p_size < (fields << 2)) {
            return TINYDELTA_ERR_VERSION; /* doesn't fit the tables buffer */
        }
        *p_size -= fields << 2;
        for (uint32_t field = 0u; (result == TINYDELTA_OK) && (field < fields); field++) {
            result = varint(p_varint_ctx, &record[field]);
        }
        if ((result == TINYDELTA_OK) && !range_within(record[0], record[1], 0xFFFFFFFFu - end)) {
            result = TINYDELTA_ERR_MALFORMED; /* past 32 bits */
        }
        record[0] += end;
        record[2] = zigzag_decode(record[2]);
        end       = record[0] + record[1];
        for (uint32_t field = 0u; field < fields; field++) {
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
    uint32_t rule             = ((uint32_t)features & TINYDELTA_CODED_RELOC_MASK) >> TINYDELTA_CODED_RELOC_SHIFT; /* Cast: widens. */
    tinydelta_result_t result = TINYDELTA_OK;

    p_reloc->b_shipped     = (features & FEATURES_SHIPPED) != 0u;
    p_reloc->run_count     = 0u;
    p_reloc->outside_count = 0u;
    if (p_reloc->b_shipped) {
#if TINYDELTA_RELOC_SHIPPED
        if (rule != FEATURES_RULE_TABLE) {
            return TINYDELTA_ERR_VERSION;
        }
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
#else
        return TINYDELTA_ERR_VERSION; /* built without shipped tables */
#endif
    } else {
        uint32_t crc = RULES_THUMB_CRC;

        if (rule == 2u) {
            crc = RULES_MSP430X_CRC;
        } else if (rule == FEATURES_RULE_TABLE) {
            uint8_t low  = 0u;
            uint8_t high = 0u;

            result = fetch(p_ctx, &low);
            if (result == TINYDELTA_OK) {
                result = fetch(p_ctx, &high);
            }
            crc = (uint32_t)low | ((uint32_t)high << 8); /* Cast: widens. */
        } else {
            /* 1: the Thumb table's */
        }
        if ((result == TINYDELTA_OK) && (crc != TINYDELTA_RELOC_RULES_CRC)) {
            result = TINYDELTA_ERR_VERSION; /* rules this decoder doesn't have */
        }
        p_reloc->p_rules   = tinydelta_reloc_rules;
        p_reloc->rules_len = TINYDELTA_RELOC_RULES_SIZE;
    }
    if (result == TINYDELTA_OK) {
        result = varint(p_ctx, &p_reloc->base);
    }
    p_reloc->p_entries = p_storage;
    if (result == TINYDELTA_OK) {
        result = read_list(varint, p_ctx, &p_storage, &storage_size, 3u, &p_reloc->count);
    }
    if (rule != 1u) { /* data runs, in 256-byte blocks; RAM ranges */
        p_reloc->p_runs = p_storage;
        if (result == TINYDELTA_OK) {
            result = read_list(varint, p_ctx, &p_storage, &storage_size, 2u, &p_reloc->run_count);
        }
        p_reloc->p_outside = p_storage;
        if (result == TINYDELTA_OK) {
            result = read_list(varint, p_ctx, &p_storage, &storage_size, 3u, &p_reloc->outside_count);
        }
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
static uint32_t address_shift(const tinydelta_reloc_t *p_reloc, uint32_t address, uint32_t space) {
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
#define NO_RULE       0u /* no rule starts at table offset 0 */

/* The table byte at offset at. */
static uint32_t tb(const tinydelta_reloc_t *p_reloc, uint32_t at) {
    return p_reloc->p_rules[at];
}

/* A little-endian value of a word's size at table offset at. */
static uint32_t table_word(const tinydelta_reloc_t *p_reloc, uint32_t at) {
    uint32_t value = 0u;

    for (uint32_t index = p_reloc->word_bytes; index > 0u; index--) {
        value = (value << 8) | tb(p_reloc, at + index - 1u);
    }
    return value;
}

/* The bits of an n-bit field, n 1 to 32. */
static uint32_t low_mask(uint32_t bits) {
    return 0xFFFFFFFFu >> ((32u - bits) & 31u);
}

/* Bytes of one condition. */
static uint32_t cond_bytes(const tinydelta_reloc_t *p_reloc) {
    return 1u + (2u * p_reloc->word_bytes);
}

/* The table offset of address k of the rule at pos (k = its address count:
 * just past the addresses). */
static uint32_t addr_at(const tinydelta_reloc_t *p_reloc, uint32_t pos, uint32_t k) {
    uint32_t at = pos + HEAD_SIZE + (((tb(p_reloc, pos + 1u) >> 2) & 7u) * cond_bytes(p_reloc));

    for (uint32_t index = 0u; index < k; index++) {
        at += 1u + (2u * tb(p_reloc, at));
    }
    return at;
}

#if TINYDELTA_RELOC_SHIPPED
/* Checks the rule at pos (whose bytes end at end) of list, of count rules;
 * *p_size gets the bytes it needs in the image. Mirrors the Go encoder's
 * parseRule, accepting and rejecting the same tables. */
static bool check_rule(const tinydelta_reloc_t *p_reloc, uint32_t pos, uint32_t end, uint32_t list, uint32_t count, uint32_t *p_size) {
    uint32_t flags = tb(p_reloc, pos + 1u);
    uint32_t attr  = tb(p_reloc, pos + 2u);
    uint32_t n     = tb(p_reloc, pos + 3u);
    uint32_t addrs = flags & 3u;
    uint32_t at    = pos + HEAD_SIZE;
    uint32_t words = 1u; /* words the rule reads, at least one */
    uint32_t bits  = 0u; /* the first address's width */
    bool b_ok      = (addrs <= 2u) && (((flags >> 2) & 7u) <= MAX_CONDS) && ((attr & 3u) <= SPACE_DATA) && ((attr & ~0x0Fu) == 0u) &&
                ((1u << ((attr >> 2) & 3u)) >= p_reloc->unit);

    if (list < LIST_CODE) {
        b_ok = b_ok && (addrs == 0u) && ((flags & (FLAG_RELATIVE | FLAG_REQUIRE | FLAG_UNLESS)) == 0u) && ((attr & 3u) == 0u) && (n >= 1u) &&
               ((list == LIST_PREFIXES) ? ((n <= MAX_SKIP) && ((n % p_reloc->unit) == 0u)) : (n <= MAX_LENGTH));
    }
    for (uint32_t index = 0u; b_ok && (index < ((flags >> 2) & 7u)); index++) {
        uint32_t word = tb(p_reloc, at) & 0x7Fu;

        b_ok = ((at + cond_bytes(p_reloc)) <= end) && (word < MAX_WORDS) &&
               ((table_word(p_reloc, at + 1u) & ~table_word(p_reloc, at + 1u + p_reloc->word_bytes)) == 0u);
        words = ((word + 1u) > words) ? (word + 1u) : words;
        at += cond_bytes(p_reloc);
    }
    for (uint32_t index = 0u; b_ok && (index < addrs); index++) {
        uint32_t slices = (at < end) ? tb(p_reloc, at) : 0u;
        uint32_t width  = 0u;

        b_ok = (slices >= 1u) && (slices <= MAX_SLICES) && ((at + 1u + (2u * slices)) <= end);
        at++;
        for (uint32_t slice = 0u; b_ok && (slice < slices); slice++) {
            uint32_t x = tb(p_reloc, at);
            uint32_t y = tb(p_reloc, at + 1u);

            if ((y & SLICE_FIXED) != 0u) {
                b_ok = ((y & 0x7Fu) < 8u) && ((x >> ((y & 0x7Fu) + 1u)) == 0u);
                width += (y & 0x7Fu) + 1u;
            } else {
                b_ok  = (y <= 31u) && (((x & 0x1Fu) + y) < (8u * p_reloc->word_bytes));
                words = (((x >> 5) + 1u) > words) ? ((x >> 5) + 1u) : words;
                width += y + 1u;
            }
            at += 2u;
        }
        b_ok = b_ok && (width <= 32u);
        bits = (index == 0u) ? width : bits;
    }
    if (b_ok && ((flags & FLAG_RELATIVE) != 0u)) {
        uint32_t sign_ext = (at + 1u < end) ? tb(p_reloc, at + 1u) : 0u;

        b_ok = (addrs == 1u) && (sign_ext >= 1u) && (sign_ext <= 31u) && (sign_ext <= bits);
        at += 2u;
    }
    b_ok = b_ok && !(((flags & FLAG_REQUIRE) != 0u) && ((addrs == 0u) || ((flags & FLAG_RELATIVE) != 0u)));
    if (b_ok && ((flags & FLAG_UNLESS) != 0u)) {
        uint32_t entries = (at < end) ? tb(p_reloc, at) : 0u;

        b_ok = (entries >= 1u) && (entries <= MAX_UNLESS);
        at++;
        for (uint32_t entry = 0u; b_ok && (entry < entries); entry++) {
            uint32_t offsets = (at + 1u < end) ? tb(p_reloc, at + 1u) : 0u;

            b_ok = ((at + 1u) < end) && (tb(p_reloc, at) < count) && (offsets >= 1u) && (offsets <= MAX_UNLESS_AT) && ((at + 2u + offsets) <= end);
            at += 2u;
            for (uint32_t index = 0u; b_ok && (index < offsets); index++) {
                uint32_t byte = tb(p_reloc, at);

                /* -16 to 16, a multiple of the unit (two's complement keeps a negative one's remainder) */
                b_ok = ((byte <= MAX_UNLESS_OFFSET) || (byte >= (256u - MAX_UNLESS_OFFSET))) && ((byte % p_reloc->unit) == 0u);
                at++;
            }
        }
    }
    *p_size = (list < LIST_CODE) ? (words * p_reloc->word_bytes) : n;
    return b_ok && (at == end) && (*p_size >= (words * p_reloc->word_bytes)) && (*p_size <= MAX_SIZE);
}

#endif

/* Reads the table's header and where its lists start; with
 * TINYDELTA_RELOC_SHIPPED, checks every rule too. A built-in table needs no
 * checks: "tinydelta rules compile" only makes valid ones. */
static bool check_table(tinydelta_reloc_t *p_reloc) {
    uint32_t pos = RULES_HEADER_SIZE;
    uint32_t mode;
    bool b_ok = (p_reloc->rules_len >= RULES_HEADER_SIZE) && (p_reloc->p_rules[0] == RULES_VERSION);

    mode                = (b_ok) ? p_reloc->p_rules[1] : 3u;
    p_reloc->unit       = 1u << (mode & 3u);
    p_reloc->word_bytes = ((mode >> 2) & 3u) + 1u;
    p_reloc->b_big      = (mode & 0x10u) != 0u;
    p_reloc->sync       = (b_ok) ? p_reloc->p_rules[2] : 0u;
    p_reloc->reach      = 0u;
    b_ok                = b_ok && ((mode & ~0x1Fu) == 0u) && ((mode & 3u) != 3u) && (p_reloc->sync <= MAX_SYNC);
    for (uint32_t list = 0u; b_ok && (list < TINYDELTA_RULE_LISTS); list++) {
        p_reloc->list_count[list] = p_reloc->p_rules[3u + list];
        p_reloc->list_start[list] = pos;
        b_ok                      = p_reloc->list_count[list] <= MAX_RULES;
        for (uint32_t index = 0u; b_ok && (index < p_reloc->list_count[list]); index++) {
            uint32_t size = 0u;
            uint32_t len  = (pos < p_reloc->rules_len) ? p_reloc->p_rules[pos] : 0u;

            b_ok = (len >= HEAD_SIZE) && (len <= (p_reloc->rules_len - pos));
#if TINYDELTA_RELOC_SHIPPED
            b_ok = b_ok && check_rule(p_reloc, pos, pos + len, list, p_reloc->list_count[list], &size);
#else
            /* The bytes a rule needs: a code or data rule's size; a prefix's or length rule's first word. */
            size = (list >= LIST_CODE) ? p_reloc->p_rules[pos + 3u] : p_reloc->word_bytes;
#endif
            if (b_ok && ((size - 1u) > p_reloc->reach)) {
                p_reloc->reach = size - 1u;
            }
            pos += len;
        }
    }
    return b_ok && (pos == p_reloc->rules_len);
}

tinydelta_result_t tinydelta_reloc_start(tinydelta_reloc_t *p_reloc, tinydelta_coder_t *p_coder) {
#if TINYDELTA_RELOC_SHIPPED
    if (p_reloc->b_shipped) {
        /* The storage's start, right after the coder's tables. */
        tinydelta_result_t result = tinydelta_coder_tree_bytes(p_coder, p_coder->p_states + tinydelta_coder_tables_size(p_coder->layout), p_reloc->rules_len);

        if (result != TINYDELTA_OK) {
            return result;
        }
    }
#else
    (void)p_coder;
#endif
    return check_table(p_reloc) ? TINYDELTA_OK : TINYDELTA_ERR_MALFORMED;
}

/* ---- the engine ----------------------------------------------------------- */

static uint8_t raw_byte(const tinydelta_reloc_t *p_reloc, uint32_t index) {
    return (p_reloc->p_dict != NULL) ? p_reloc->p_dict[index] : p_reloc->raw(p_reloc->p_raw_ctx, index);
}

/* Word w of a rule at position p (the caller checked the bounds). */
static uint32_t word_at(const tinydelta_reloc_t *p_reloc, uint32_t p, uint32_t w) {
    uint32_t at    = p + (w * p_reloc->word_bytes);
    uint32_t value = 0u;

    for (uint32_t index = 0u; index < p_reloc->word_bytes; index++) {
        uint32_t byte = raw_byte(p_reloc, at + (p_reloc->b_big ? index : (p_reloc->word_bytes - 1u - index)));

        value = (value << 8) | byte;
    }
    return value;
}

/* Whether n bytes from p are inside the image. */
static bool inside(const tinydelta_reloc_t *p_reloc, uint32_t p, uint32_t n) {
    return (n <= p_reloc->dict_len) && (p <= (p_reloc->dict_len - n));
}

/* Whether the rule at pos is aligned at p and its conditions hold there,
 * the words they read inside the image. */
static bool matches(const tinydelta_reloc_t *p_reloc, uint32_t pos, uint32_t p) {
    uint32_t at = pos + HEAD_SIZE;
    bool b_ok   = ((p_reloc->base + p) & ((1u << ((tb(p_reloc, pos + 2u) >> 2) & 3u)) - 1u)) == 0u;

    for (uint32_t index = ((tb(p_reloc, pos + 1u) >> 2) & 7u); b_ok && (index > 0u); index--) {
        uint32_t word = tb(p_reloc, at) & 0x7Fu;

        b_ok = inside(p_reloc, p, (word + 1u) * p_reloc->word_bytes) && (((word_at(p_reloc, p, word) & table_word(p_reloc, at + 1u + p_reloc->word_bytes)) ==
                                                                          table_word(p_reloc, at + 1u)) != ((tb(p_reloc, at) & 0x80u) != 0u));
        at += cond_bytes(p_reloc);
    }
    return b_ok;
}

/* An address's value at p (its slices at table offset at, most significant
 * first); its width in *p_bits. */
static uint32_t join(const tinydelta_reloc_t *p_reloc, uint32_t at, uint32_t p, uint32_t *p_bits) {
    uint32_t value = 0u;

    *p_bits = 0u;
    for (uint32_t index = tb(p_reloc, at); index > 0u; index--) {
        uint32_t x     = tb(p_reloc, at + 1u);
        uint32_t y     = tb(p_reloc, at + 2u);
        uint32_t width = (y & 0x7Fu) + 1u;
        uint32_t part  = x;

        if ((y & SLICE_FIXED) == 0u) {
            part = (word_at(p_reloc, p, x >> 5) >> (x & 0x1Fu)) & low_mask(width);
        }
        value = (width >= 32u) ? part : ((value << width) | part);
        *p_bits += width;
        at += 2u;
    }
    return value;
}

/* Writes value into an address's slices at p, the bytes of them inside the
 * span; nothing if it doesn't fit them or their fixed bits. */
static void split(const tinydelta_reloc_t *p_reloc, const span_t *p_span, uint32_t at, uint32_t p, uint32_t value) {
    uint32_t count = tb(p_reloc, at);
    bool b_fits    = true;

    /* The slices from the last (least significant): check, then write. */
    for (uint32_t pass = 0u; b_fits && (pass < 2u); pass++) {
        uint32_t rest = value;

        for (uint32_t index = count; b_fits && (index > 0u); index--) {
            uint32_t x     = tb(p_reloc, at + (2u * index) - 1u);
            uint32_t y     = tb(p_reloc, at + (2u * index));
            uint32_t width = (y & 0x7Fu) + 1u;
            uint32_t part  = rest & low_mask(width);

            if ((y & SLICE_FIXED) != 0u) {
                b_fits = part == x;
            } else if (pass == 1u) {
                uint32_t mask = low_mask(width) << (x & 0x1Fu);
                uint32_t word = p + ((x >> 5) * p_reloc->word_bytes);

                part <<= x & 0x1Fu;
                for (uint32_t byte_index = 0u; byte_index < p_reloc->word_bytes; byte_index++) {
                    /* Byte byte_index of the word (least significant first) and where it is in the span; wraps below it. */
                    uint32_t position = (word + (p_reloc->b_big ? (p_reloc->word_bytes - 1u - byte_index) : byte_index)) - p_span->offset;

                    if (position < p_span->length) {
                        /* Cast: keeps the low byte. Only the slice's bits change. */
                        p_span->p_out[position] = (uint8_t)((p_span->p_out[position] & ~(mask & 0xFFu)) | (part & mask & 0xFFu));
                    }
                    mask >>= 8;
                    part >>= 8;
                }
            } else {
                /* checking only */
            }
            rest = (width >= 32u) ? 0u : (rest >> width);
        }
    }
}

/* The first rule of a list matching at p: its table offset (NO_RULE if
 * none) and its index in *p_index. A prefix or length rule must fit the
 * image; one with require counts only if it fits and its first address
 * points into the image. */
static uint32_t first_rule(const tinydelta_reloc_t *p_reloc, uint32_t list, uint32_t p, uint32_t *p_index) {
    uint32_t pos = p_reloc->list_start[list];

    for (uint32_t index = 0u; index < p_reloc->list_count[list]; index++) {
        uint32_t flags = tb(p_reloc, pos + 1u);
        uint32_t bits;

        if (matches(p_reloc, pos, p) && ((list >= LIST_CODE) || inside(p_reloc, p, p_reloc->word_bytes)) &&
            (((flags & FLAG_REQUIRE) == 0u) ||
             (inside(p_reloc, p, tb(p_reloc, pos + 3u)) && in_image(p_reloc, join(p_reloc, addr_at(p_reloc, pos, 0u), p, &bits))))) {
            *p_index = index;
            return pos;
        }
        pos += tb(p_reloc, pos);
    }
    return NO_RULE;
}

/* The instruction length at q for finding instruction starts: 0 outside
 * the image, at a misaligned position or in a data run. */
static uint32_t length_at(const tinydelta_reloc_t *p_reloc, uint32_t q) {
    uint32_t index;
    uint32_t pos;
    uint32_t skip = 0u;

    if (!inside(p_reloc, q, p_reloc->word_bytes) || (((p_reloc->base + q) & (p_reloc->unit - 1u)) != 0u) || in_data(p_reloc, q)) {
        return 0u;
    }
    pos = first_rule(p_reloc, LIST_PREFIXES, q, &index);
    if ((pos != NO_RULE) && inside(p_reloc, q, tb(p_reloc, pos + 3u) + p_reloc->word_bytes)) {
        skip = tb(p_reloc, pos + 3u);
    }
    pos = first_rule(p_reloc, LIST_LENGTHS, q + skip, &index);
    return skip + ((pos != NO_RULE) ? tb(p_reloc, pos + 3u) : p_reloc->unit);
}

/* Whether p starts an instruction: decoding lengths from sync units before
 * lands on it. */
static bool starts(const tinydelta_reloc_t *p_reloc, uint32_t p) {
    uint32_t covered = 0u; /* bit i: the unit i units on is inside an instruction */

    for (uint32_t i = p_reloc->sync; i > 0u; i--) {
        if (((covered & 1u) == 0u) && ((i * p_reloc->unit) <= p)) {
            uint32_t units = length_at(p_reloc, p - (i * p_reloc->unit)) / p_reloc->unit;

            if (units > 0u) {
                covered |= low_mask(units) & ~1u;
            }
        }
        covered >>= 1;
    }
    return (covered & 1u) == 0u;
}

/* The rule that applies at p before unless: its table offset (NO_RULE if
 * none, or an exclusion), its list and index. */
static uint32_t applies(const tinydelta_reloc_t *p_reloc, uint32_t p, uint32_t *p_list, uint32_t *p_index) {
    uint32_t pos;

    *p_list = in_data(p_reloc, p) ? LIST_DATA : LIST_CODE;
    if ((p >= p_reloc->dict_len) || (((p_reloc->base + p) & (p_reloc->unit - 1u)) != 0u)) {
        return NO_RULE;
    }
    pos = first_rule(p_reloc, *p_list, p, p_index);
    if ((pos == NO_RULE) || ((tb(p_reloc, pos + 1u) & 3u) == 0u) || !inside(p_reloc, p, tb(p_reloc, pos + 3u)) ||
        ((*p_list == LIST_CODE) && (p_reloc->sync > 0u) && !starts(p_reloc, p))) {
        return NO_RULE;
    }
    return pos;
}

/* Whether the rule at pos, which applies at p in list, is excluded by its
 * unless entries: their rule applies at one of their offsets. */
static bool excluded(const tinydelta_reloc_t *p_reloc, uint32_t pos, uint32_t list, uint32_t p) {
    uint32_t flags  = tb(p_reloc, pos + 1u);
    uint32_t at     = addr_at(p_reloc, pos, flags & 3u) + (((flags & FLAG_RELATIVE) != 0u) ? 2u : 0u);
    bool b_excluded = false;

    if ((flags & FLAG_UNLESS) != 0u) {
        for (uint32_t entry = tb(p_reloc, at); entry > 0u; entry--) {
            uint32_t rule    = tb(p_reloc, at + 1u);
            uint32_t offsets = tb(p_reloc, at + 2u);

            at += 3u;
            for (; offsets > 0u; offsets--) {
                uint32_t other_list;
                uint32_t other;
                /* Cast: two's complement, so a negative offset wraps below p (past the image if p is near 0). */
                uint32_t q = p + ((tb(p_reloc, at) >= 0x80u) ? ((uint32_t)tb(p_reloc, at) | 0xFFFFFF00u) : (uint32_t)tb(p_reloc, at));

                if ((applies(p_reloc, q, &other_list, &other) != NO_RULE) && (other == rule) && (other_list == list)) {
                    b_excluded = true;
                }
                at++;
            }
        }
    }
    return b_excluded;
}

/* Whether a + shift (two's complement) stays within 0 to 2^32 - 1. */
static bool no_wrap(uint32_t a, uint32_t shift) {
    return ((shift & 0x80000000u) != 0u) ? ((a + shift) < a) : ((a + shift) >= a);
}

/* Rewrites the addresses of the rule at pos, at p, in the span. */
static void move_at(const tinydelta_reloc_t *p_reloc, uint32_t pos, uint32_t p, const span_t *p_span) {
    uint32_t flags = tb(p_reloc, pos + 1u);
    uint32_t space = tb(p_reloc, pos + 2u) & 3u;
    uint32_t bits;

    if ((flags & FLAG_RELATIVE) != 0u) {
        uint32_t at       = addr_at(p_reloc, pos, 0u);
        uint32_t rel      = addr_at(p_reloc, pos, 1u);
        uint32_t sign_ext = tb(p_reloc, rel + 1u);
        uint32_t sign     = 1u << (sign_ext - 1u);
        /* The offset, sign-extended (two's complement), and the site's address. */
        uint32_t offset   = ((join(p_reloc, at, p, &bits) & low_mask(sign_ext)) ^ sign) - sign;
        uint32_t site     = p_reloc->base + p;
        uint32_t delta    = offset + ((tb(p_reloc, rel) >= 0x80u) ? (tb(p_reloc, rel) | 0xFFFFFF00u) : tb(p_reloc, rel)); /* base + offset, |.| < 2^31 */
        uint32_t next     = offset - address_shift(p_reloc, site, SPACE_CODE);

        if (no_wrap(site, delta)) {
            next += address_shift(p_reloc, site + delta, space);
        }
        /* Within sign_ext bits (as two's complement), and changed. */
        if ((next != offset) && ((next + sign) < (sign << 1))) {
            split(p_reloc, p_span, at, p, next & low_mask(bits));
        }
    } else {
        for (uint32_t index = 0u; index < (flags & 3u); index++) {
            uint32_t at      = addr_at(p_reloc, pos, index);
            uint32_t address = join(p_reloc, at, p, &bits);
            uint32_t shift   = address_shift(p_reloc, address, space);

            /* Not negative nor past 32 bits, and within the slices' bits. */
            if ((shift != 0u) && no_wrap(address, shift) && ((bits >= 32u) || (((address + shift) >> bits) == 0u))) {
                split(p_reloc, p_span, at, p, address + shift);
            }
        }
    }
}

void tinydelta_reloc_span(const tinydelta_reloc_t *p_reloc, uint32_t offset, uint8_t *p_out, uint32_t length) {
    span_t span  = {offset, length, p_out};
    uint32_t end = offset + length;

    for (uint32_t p = (offset >= p_reloc->reach) ? (offset - p_reloc->reach) : 0u; p < end; p++) {
        uint32_t list;
        uint32_t index;
        uint32_t pos = applies(p_reloc, p, &list, &index);

        if ((pos != NO_RULE) && !excluded(p_reloc, pos, list, p)) {
            move_at(p_reloc, pos, p, &span);
        }
    }
}

#endif
