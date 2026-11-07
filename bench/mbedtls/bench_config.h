/* SPDX-FileCopyrightText: 2026 Simon Bjerg
 * SPDX-License-Identifier: MIT
 */

/* mbedtls configuration for the benchmark's firmware images (bench/collect.py,
 * collector "mbedtls"): the crypto a small device's firmware update or
 * metering protocol uses, no TLS, no PSA, no operating system. Option names
 * are those of mbedtls 2.28 and 3.6 (MBEDTLS_SHA224_C and MBEDTLS_SHA384_C are
 * 3.x's; 2.28 has them in MBEDTLS_SHA256_C and MBEDTLS_SHA512_C). */
#ifndef BENCH_CONFIG_H
#define BENCH_CONFIG_H

/* Memory: a static pool in main.c, no C library malloc. */
#define MBEDTLS_PLATFORM_C
#define MBEDTLS_PLATFORM_MEMORY
#define MBEDTLS_PLATFORM_CALLOC_MACRO bench_calloc
#define MBEDTLS_PLATFORM_FREE_MACRO   bench_free
#include <limits.h> /* some 2.28 commits use INT_MAX without including it */
#include <stddef.h>
void *bench_calloc(size_t count, size_t size);
void bench_free(void *pointer);

/* Symmetric: AES (tables in ROM, as const data) with CBC, CTR, GCM, CCM, CMAC; ChaCha20-Poly1305. */
#define MBEDTLS_AES_C
#define MBEDTLS_AES_ROM_TABLES
#define MBEDTLS_CIPHER_C
#define MBEDTLS_CIPHER_MODE_CBC
#define MBEDTLS_CIPHER_MODE_CTR
#define MBEDTLS_GCM_C
#define MBEDTLS_CCM_C
#define MBEDTLS_CMAC_C
#define MBEDTLS_CHACHA20_C
#define MBEDTLS_POLY1305_C
#define MBEDTLS_CHACHAPOLY_C

/* Hashes and MACs. */
#define MBEDTLS_MD_C
#define MBEDTLS_SHA1_C
#define MBEDTLS_SHA224_C
#define MBEDTLS_SHA256_C
#define MBEDTLS_SHA384_C
#define MBEDTLS_SHA512_C
#define MBEDTLS_HMAC_DRBG_C
#define MBEDTLS_CTR_DRBG_C
#define MBEDTLS_HKDF_C

/* Public key: ECDSA and ECDH on P-256, RSA verification. */
#define MBEDTLS_BIGNUM_C
#define MBEDTLS_ECP_C
#define MBEDTLS_ECP_DP_SECP256R1_ENABLED
#define MBEDTLS_ECDSA_C
#define MBEDTLS_ECDSA_DETERMINISTIC
#define MBEDTLS_ECDH_C
#define MBEDTLS_RSA_C
#define MBEDTLS_PKCS1_V15
#define MBEDTLS_PKCS1_V21
#define MBEDTLS_OID_C
#define MBEDTLS_ASN1_PARSE_C
#define MBEDTLS_ASN1_WRITE_C
#define MBEDTLS_BASE64_C

#endif
