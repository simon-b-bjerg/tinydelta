/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* The benchmark's firmware program (bench/collect.py, collector "mbedtls"):
 * the same calls into mbedtls at every commit, so each image is the library
 * as a real firmware links it, and two images differ only where mbedtls
 * changed. It is linked, never run: the calls and the table of function
 * pointers (as an application's dispatch table) keep the code in the image
 * and give it the calls and pointers relocation moves. */
#include <stdint.h>
#include <string.h>

#include "mbedtls/aes.h"
#include "mbedtls/ccm.h"
#include "mbedtls/chachapoly.h"
#include "mbedtls/cmac.h"
#include "mbedtls/ctr_drbg.h"
#include "mbedtls/ecdh.h"
#include "mbedtls/ecdsa.h"
#include "mbedtls/gcm.h"
#include "mbedtls/hkdf.h"
#include "mbedtls/hmac_drbg.h"
#include "mbedtls/md.h"
#include "mbedtls/rsa.h"
#include "mbedtls/sha1.h"
#include "mbedtls/sha256.h"
#include "mbedtls/sha512.h"

#define BENCH_POOL_SIZE 2048u

static uint8_t bench_pool[BENCH_POOL_SIZE];
static uint32_t bench_pool_used;

/* A bump allocator: enough to link, never run. */
void *bench_calloc(size_t count, size_t size) {
    uint32_t bytes = (uint32_t)(count * size);
    if (bytes > BENCH_POOL_SIZE - bench_pool_used) {
        return NULL;
    }
    bench_pool_used += bytes;
    return memset(&bench_pool[bench_pool_used - bytes], 0, bytes);
}

void bench_free(void *pointer) {
    (void)pointer;
}

static int bench_random(void *context, unsigned char *output, size_t length) {
    (void)context;
    memset(output, 0x5A, length);
    return 0;
}

static uint8_t key[32], iv[16], input[64], output[64], tag[16], hash[64];

static int symmetric(void) {
    mbedtls_aes_context aes;
    mbedtls_gcm_context gcm;
    mbedtls_ccm_context ccm;
    mbedtls_chachapoly_context chachapoly;
    size_t offset = 0;
    uint8_t stream[16];
    int result = 0;

    mbedtls_aes_init(&aes);
    result |= mbedtls_aes_setkey_enc(&aes, key, 128);
    result |= mbedtls_aes_crypt_cbc(&aes, MBEDTLS_AES_ENCRYPT, sizeof input, iv, input, output);
    result |= mbedtls_aes_crypt_ctr(&aes, sizeof input, &offset, iv, stream, input, output);
    result |= mbedtls_aes_setkey_dec(&aes, key, 256);
    result |= mbedtls_aes_crypt_ecb(&aes, MBEDTLS_AES_DECRYPT, input, output);
    mbedtls_aes_free(&aes);

    mbedtls_gcm_init(&gcm);
    result |= mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key, 128);
    result |= mbedtls_gcm_crypt_and_tag(&gcm, MBEDTLS_GCM_ENCRYPT, sizeof input, iv, 12, NULL, 0, input, output, sizeof tag, tag);
    result |= mbedtls_gcm_auth_decrypt(&gcm, sizeof input, iv, 12, NULL, 0, tag, sizeof tag, input, output);
    mbedtls_gcm_free(&gcm);

    mbedtls_ccm_init(&ccm);
    result |= mbedtls_ccm_setkey(&ccm, MBEDTLS_CIPHER_ID_AES, key, 128);
    result |= mbedtls_ccm_encrypt_and_tag(&ccm, sizeof input, iv, 13, NULL, 0, input, output, tag, 8);
    result |= mbedtls_ccm_auth_decrypt(&ccm, sizeof input, iv, 13, NULL, 0, input, output, tag, 8);
    mbedtls_ccm_free(&ccm);

    result |= mbedtls_cipher_cmac(mbedtls_cipher_info_from_type(MBEDTLS_CIPHER_AES_128_ECB), key, 128, input, sizeof input, tag);

    mbedtls_chachapoly_init(&chachapoly);
    result |= mbedtls_chachapoly_setkey(&chachapoly, key);
    result |= mbedtls_chachapoly_encrypt_and_tag(&chachapoly, sizeof input, iv, NULL, 0, input, output, tag);
    mbedtls_chachapoly_free(&chachapoly);
    return result;
}

static int hashes(void) {
    const mbedtls_md_info_t *sha256 = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
    int result = 0;

    result |= mbedtls_sha1(input, sizeof input, hash);
    result |= mbedtls_sha256(input, sizeof input, hash, 0);
    result |= mbedtls_sha512(input, sizeof input, hash, 1);
    result |= mbedtls_md_hmac(sha256, key, sizeof key, input, sizeof input, hash);
    result |= mbedtls_hkdf(sha256, NULL, 0, key, sizeof key, NULL, 0, output, 32);
    return result;
}

static int public_key(void) {
    mbedtls_ecp_group group;
    mbedtls_ecp_point point;
    mbedtls_mpi r, s, d, shared;
    mbedtls_rsa_context rsa;
    mbedtls_ctr_drbg_context ctr_drbg;
    mbedtls_hmac_drbg_context hmac_drbg;
    int result = 0;

    mbedtls_ecp_group_init(&group);
    mbedtls_ecp_point_init(&point);
    mbedtls_mpi_init(&r);
    mbedtls_mpi_init(&s);
    mbedtls_mpi_init(&d);
    mbedtls_mpi_init(&shared);
    result |= mbedtls_ecp_group_load(&group, MBEDTLS_ECP_DP_SECP256R1);
    result |= mbedtls_ecp_gen_keypair(&group, &d, &point, bench_random, NULL);
    result |= mbedtls_ecdsa_sign_det_ext(&group, &r, &s, &d, hash, 32, MBEDTLS_MD_SHA256, bench_random, NULL);
    result |= mbedtls_ecdsa_verify(&group, hash, 32, &point, &r, &s);
    result |= mbedtls_ecdh_compute_shared(&group, &shared, &point, &d, bench_random, NULL);
    mbedtls_ecp_group_free(&group);
    mbedtls_ecp_point_free(&point);

    mbedtls_rsa_init(&rsa);
    result |= mbedtls_rsa_import_raw(&rsa, key, sizeof key, NULL, 0, NULL, 0, NULL, 0, iv, 3);
    result |= mbedtls_rsa_complete(&rsa);
    result |= mbedtls_rsa_pkcs1_verify(&rsa, MBEDTLS_MD_SHA256, 32, hash, output);
    mbedtls_rsa_free(&rsa);

    mbedtls_ctr_drbg_init(&ctr_drbg);
    result |= mbedtls_ctr_drbg_seed(&ctr_drbg, bench_random, NULL, key, sizeof key);
    result |= mbedtls_ctr_drbg_random(&ctr_drbg, output, sizeof output);
    mbedtls_ctr_drbg_free(&ctr_drbg);

    mbedtls_hmac_drbg_init(&hmac_drbg);
    result |= mbedtls_hmac_drbg_seed_buf(&hmac_drbg, mbedtls_md_info_from_type(MBEDTLS_MD_SHA256), key, sizeof key);
    result |= mbedtls_hmac_drbg_random(&hmac_drbg, output, sizeof output);
    mbedtls_hmac_drbg_free(&hmac_drbg);
    return result;
}

/* A dispatch table of function pointers in constant data, as an
 * application's command table. */
typedef int (*bench_command_t)(void);
const bench_command_t bench_commands[] = {symmetric, hashes, public_key};

int main(void) {
    int result = 0;
    for (uint32_t index = 0; index < sizeof bench_commands / sizeof bench_commands[0]; index++) {
        result |= bench_commands[index]();
    }
    return result;
}
