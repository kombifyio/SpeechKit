/*  blas_i8_avx2.h – SpeechKit's x86 port of blas.c's int8 dot-product GEMM, included by blas.c in place of the
 *  NEON SDOT block when the build has AVX2 and no dot-product extension. Same modes, layouts and arithmetic:
 *
 *  A: [Mp/4 tiles][K/4 quads][4 rows][4 k] int8, quantised per row (scale = max|row| / 127, round to nearest even).
 *  B: per 16-column panel, [K/4 quads][16 columns][4 k] int8 (S and T planes, or the int8w codes, or an int8 table).
 *
 *  AVX2 has no signed × signed byte dot product. vpmaddubsw multiplies unsigned by signed bytes into pairwise int16
 *  sums, so each product is taken as |b| · (a with b's sign): A is within ±127, so negating it never overflows, and a
 *  weight byte of −128 becomes 128 as unsigned; a pair sum stays within ±32512, below int16 saturation, and vpmaddwd
 *  against ones widens it to the int32 sum of four. |b| is taken once per quad for all four rows. The sums are exact,
 *  so the result equals the AArch64 int8 path's. Not a separate translation unit: it uses blas.c's statics (NR, scale_c, the pool).
 */

/* Per-thread buffers: the quantised A (grows with M × K) and the packed B block (fixed, two planes). */
typedef struct {
    signed char *a;      float *a_scales; size_t a_cap; int a_rows;
    signed char *b;      size_t b_cap;
} I8Bufs;

static pthread_key_t  tls_i8_key;
static pthread_once_t tls_i8_once = PTHREAD_ONCE_INIT;

static void tls_i8_destroy(void *p)
{
    I8Bufs *b = (I8Bufs *)p;
    if (b) { p2_aligned_free(b->a); free(b->a_scales); p2_aligned_free(b->b); free(b); }
}

static void tls_i8_init(void) { pthread_key_create(&tls_i8_key, tls_i8_destroy); }

static I8Bufs *get_i8_bufs(size_t a_bytes, int a_rows, size_t b_bytes)
{
    pthread_once(&tls_i8_once, tls_i8_init);
    I8Bufs *b = (I8Bufs *)pthread_getspecific(tls_i8_key);
    if (!b) {
        b = (I8Bufs *)calloc(1, sizeof(*b));
        if (!b) return NULL;
        pthread_setspecific(tls_i8_key, b);
    }
    if (a_bytes > b->a_cap || a_rows > b->a_rows) {
        p2_aligned_free(b->a); free(b->a_scales);
        b->a = (signed char *)p2_aligned_alloc(64, (a_bytes + 63) / 64 * 64);
        b->a_scales = (float *)malloc((size_t)a_rows * sizeof(float));
        b->a_cap = a_bytes; b->a_rows = a_rows;
        if (!b->a || !b->a_scales) { b->a_cap = 0; b->a_rows = 0; return NULL; }
    }
    if (b_bytes > b->b_cap) {
        p2_aligned_free(b->b);
        b->b = (signed char *)p2_aligned_alloc(64, (b_bytes + 63) / 64 * 64);
        b->b_cap = b_bytes;
        if (!b->b) { b->b_cap = 0; return NULL; }
    }
    return b;
}

/* Quantise one row of K floats symmetrically to int8 and scatter it into the tile layout: dst0 is the row's first
 * quad (tile base + 4·r), successive quads are 16 bytes apart. */
static inline void quant_pack_row(const float *row, int K, signed char *dst0, float *scale_out)
{
    const __m256 absmask = _mm256_castsi256_ps(_mm256_set1_epi32(0x7fffffff));
    __m256 vmax = _mm256_setzero_ps();
    int k = 0;
    for (; k + 8 <= K; k += 8) vmax = _mm256_max_ps(vmax, _mm256_and_ps(_mm256_loadu_ps(row + k), absmask));
    __m128 m4 = _mm_max_ps(_mm256_castps256_ps128(vmax), _mm256_extractf128_ps(vmax, 1));
    m4 = _mm_max_ps(m4, _mm_movehl_ps(m4, m4));
    m4 = _mm_max_ss(m4, _mm_shuffle_ps(m4, m4, 1));
    float amax = _mm_cvtss_f32(m4);
    for (; k < K; k++) { float v = fabsf(row[k]); if (v > amax) amax = v; }
    float scale = amax > 0.0f ? amax / 127.0f : 1.0f;
    *scale_out = scale;
    __m256 vinv = _mm256_set1_ps(1.0f / scale);
    k = 0;
    for (; k + 16 <= K; k += 16) {
        /* cvtps rounds to nearest even, as vcvtnq does; packs saturates as vqmovn does */
        __m256i i0 = _mm256_cvtps_epi32(_mm256_mul_ps(_mm256_loadu_ps(row + k),     vinv));
        __m256i i1 = _mm256_cvtps_epi32(_mm256_mul_ps(_mm256_loadu_ps(row + k + 8), vinv));
        __m128i w0 = _mm_packs_epi32(_mm256_castsi256_si128(i0), _mm256_extracti128_si256(i0, 1));
        __m128i w1 = _mm_packs_epi32(_mm256_castsi256_si128(i1), _mm256_extracti128_si256(i1, 1));
        /* NaN or Inf converts to INT_MIN, which would pack to −128; keep the kernel's ±127 bound */
        __m128i q  = _mm_max_epi8(_mm_packs_epi16(w0, w1), _mm_set1_epi8(-127));
        signed char *dst = dst0 + (size_t)(k / 4) * 16;
        uint32_t quads[4];
        _mm_storeu_si128((__m128i *)quads, q);
        memcpy(dst,      &quads[0], 4);
        memcpy(dst + 16, &quads[1], 4);
        memcpy(dst + 32, &quads[2], 4);
        memcpy(dst + 48, &quads[3], 4);
    }
    for (; k < K; k++) {
        int v = (int)lrintf(row[k] / scale);
        if (v > 127) v = 127; else if (v < -127) v = -127;
        dst0[(size_t)(k / 4) * 16 + (k & 3)] = (signed char)v;
    }
}

static inline void zero_pad_row(int K, signed char *dst0)
{
    for (int kq = 0; kq < K / 4; kq++) memset(dst0 + (size_t)kq * 16, 0, 4);
}

static inline signed char *row_quad0(signed char *Ap, int K, int m)
{
    return Ap + ((size_t)(m / 4) * (K / 4)) * 16 + (m % 4) * 4;
}

/* Quantise rows of A into the quad layout, one 4-row tile at a time (parallel over tiles). */
struct quant_args { const float *A; int M, K, lda; signed char *Ap; float *scales; };

static void quant_rows_worker(int tid, int nt, void *aux)
{
    struct quant_args *a = (struct quant_args *)aux;
    int tiles = (a->M + 3) / 4;
    int t0 = (tiles * tid) / nt, t1 = (tiles * (tid + 1)) / nt;
    for (int t = t0; t < t1; t++)
        for (int r = 0; r < 4; r++) {
            int m = 4 * t + r;
            signed char *dst0 = row_quad0(a->Ap, a->K, m);
            if (m >= a->M) zero_pad_row(a->K, dst0);
            else quant_pack_row(a->A + (size_t)m * a->lda, a->K, dst0, a->scales + m);
        }
}

int p2_quant_prepare(QuantA *qa, int M, int K)
{
    if (use_exact() || K % 4 != 0) return -1;
    int Mp = (M + 3) / 4 * 4;
    I8Bufs *b = get_i8_bufs((size_t)Mp * K, Mp, 0);
    if (!b) return -1;
    qa->q = b->a; qa->scales = b->a_scales; qa->M = M; qa->K = K;
    for (int m = M; m < Mp; m++) zero_pad_row(K, row_quad0(b->a, K, m));
    return 0;
}

void p2_quant_row(const QuantA *qa, int m, const float *row)
{
    quant_pack_row(row, qa->K, row_quad0(qa->q, qa->K, m), qa->scales + m);
}

/* Sixteen columns of one five-value row → S (sign) and T (sign · flag) as int8 in {−1, 0, +1}, and the flag as a
 * byte mask. Lane i takes sign code (signs[i/4] >> 2·(i%4)) & 3 and flag bit (flags[i/8] >> (i%8)) & 1. */
static inline void fv_row16_i8(const FiveValueB *b, int N, int k, int col, __m128i *S, __m128i *T, __m128i *F)
{
    const __m128i rep4 = _mm_setr_epi8(0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3);
    const __m128i sel2 = _mm_setr_epi8(3, 12, 48, -64, 3, 12, 48, -64, 3, 12, 48, -64, 3, 12, 48, -64);
    const __m128i two2 = _mm_setr_epi8(2, 8, 32, -128, 2, 8, 32, -128, 2, 8, 32, -128, 2, 8, 32, -128);
    const __m128i rep8 = _mm_setr_epi8(0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1);
    const __m128i bit1 = _mm_setr_epi8(1, 2, 4, 8, 16, 32, 64, -128, 1, 2, 4, 8, 16, 32, 64, -128);
    uint32_t s32; uint16_t f16;
    memcpy(&s32, b->signs + (size_t)k * (N / 4) + (col >> 2), 4);
    memcpy(&f16, b->flags + (size_t)k * (N / 8) + (col >> 3), 2);
    __m128i code = _mm_and_si128(_mm_shuffle_epi8(_mm_cvtsi32_si128((int)s32), rep4), sel2);
    /* code 0 → −1, 1 → 0, 2 → +1: (code == 0) is −1, (code == 2) is −1, their difference the sign */
    __m128i sgn = _mm_sub_epi8(_mm_cmpeq_epi8(code, _mm_setzero_si128()), _mm_cmpeq_epi8(code, two2));
    __m128i flg = _mm_and_si128(_mm_shuffle_epi8(_mm_cvtsi32_si128((int)f16), rep8), bit1);
    __m128i fmask = _mm_cmpeq_epi8(flg, bit1);
    *S = sgn;
    *T = _mm_and_si128(sgn, fmask);
    *F = fmask;
}

/* Four rows of sixteen columns, r0..r3, into one quad of the panel: [16 columns][4 k], what vst4q_s8 writes. */
static inline void store_quad(signed char *dst, __m128i r0, __m128i r1, __m128i r2, __m128i r3)
{
    __m128i a = _mm_unpacklo_epi8(r0, r1), b = _mm_unpackhi_epi8(r0, r1);
    __m128i c = _mm_unpacklo_epi8(r2, r3), d = _mm_unpackhi_epi8(r2, r3);
    _mm_storeu_si128((__m128i *)(dst),      _mm_unpacklo_epi16(a, c));
    _mm_storeu_si128((__m128i *)(dst + 16), _mm_unpackhi_epi16(a, c));
    _mm_storeu_si128((__m128i *)(dst + 32), _mm_unpacklo_epi16(b, d));
    _mm_storeu_si128((__m128i *)(dst + 48), _mm_unpackhi_epi16(b, d));
}

/* Rows [k0, k0 + kb) of one 16-column five-value panel → S and T planes, [kb/4 quads][16 cols][4 k]. */
static void pack_fv_panel(const FiveValueB *b, int N, int k0, int kb, int col, signed char *S, signed char *T)
{
    for (int kq = 0; kq < kb / 4; kq++) {
        int k = k0 + 4 * kq;
        __m128i s[4], t[4], f;
        for (int i = 0; i < 4; i++) fv_row16_i8(b, N, k + i, col, &s[i], &t[i], &f);
        store_quad(S + (size_t)kq * 64, s[0], s[1], s[2], s[3]);
        store_quad(T + (size_t)kq * 64, t[0], t[1], t[2], t[3]);
    }
}

/* The same rows as one plane of int8 weight codes: sign · (flag ? 127 : clo[n]), clo[n] = round(127 · lo[n] / hi[n]),
 * to be scaled by hi[n] / 127. */
static void pack_fv_codes_panel(const FiveValueB *b, int N, int k0, int kb, int col, __m128i clo, signed char *P)
{
    const __m128i v127 = _mm_set1_epi8(127);
    for (int kq = 0; kq < kb / 4; kq++) {
        int k = k0 + 4 * kq;
        __m128i c[4];
        for (int i = 0; i < 4; i++) {
            __m128i s, t, f;
            fv_row16_i8(b, N, k + i, col, &s, &t, &f);
            c[i] = _mm_sign_epi8(_mm_blendv_epi8(clo, v127, f), s);
        }
        store_quad(P + (size_t)kq * 64, c[0], c[1], c[2], c[3]);
    }
}

/* Rows [k0, k0 + kb) of one 16-column int8 panel (row stride ldb). */
static void pack_q8_panel(const signed char *B, int ldb, int k0, int kb, int col, signed char *P)
{
    const signed char *src = B + (size_t)k0 * ldb + col;
    for (int kq = 0; kq < kb / 4; kq++)
        store_quad(P + (size_t)kq * 64,
                   _mm_loadu_si128((const __m128i *)(src + (size_t)(4 * kq)     * ldb)),
                   _mm_loadu_si128((const __m128i *)(src + (size_t)(4 * kq + 1) * ldb)),
                   _mm_loadu_si128((const __m128i *)(src + (size_t)(4 * kq + 2) * ldb)),
                   _mm_loadu_si128((const __m128i *)(src + (size_t)(4 * kq + 3) * ldb)));
}

/* One 4 × 16 tile over the whole K: acc[r][0] holds columns 0..7 of row r, acc[r][1] columns 8..15. */
static inline void kernel_i8_4x16(const signed char *Ap, const signed char *Bp, int Kq, __m256i acc[4][2])
{
    const __m256i ones = _mm256_set1_epi16(1);
    for (int r = 0; r < 4; r++) acc[r][0] = acc[r][1] = _mm256_setzero_si256();
    for (int kq = 0; kq < Kq; kq++) {
        const signed char *bp = Bp + (size_t)kq * 64;
        __m256i b0 = _mm256_loadu_si256((const __m256i *)bp);
        __m256i b1 = _mm256_loadu_si256((const __m256i *)(bp + 32));
        __m256i u0 = _mm256_abs_epi8(b0), u1 = _mm256_abs_epi8(b1);   /* −128 → 128 as unsigned */
        const signed char *ap = Ap + (size_t)kq * 16;
        for (int r = 0; r < 4; r++) {
            int32_t q;
            memcpy(&q, ap + 4 * r, 4);
            __m256i a  = _mm256_set1_epi32(q);
            __m256i p0 = _mm256_maddubs_epi16(u0, _mm256_sign_epi8(a, b0));
            __m256i p1 = _mm256_maddubs_epi16(u1, _mm256_sign_epi8(a, b1));
            acc[r][0] = _mm256_add_epi32(acc[r][0], _mm256_madd_epi16(p0, ones));
            acc[r][1] = _mm256_add_epi32(acc[r][1], _mm256_madd_epi16(p1, ones));
        }
    }
}

struct sgemm_i8_args {
    int M, N, K;
    float alpha;
    const signed char *Ap; const float *a_scales;
    float *C; int ldc;
    const FiveValueB *fv;                                   /* five-value */
    int codes;                                              /* five-value as one plane of int8 codes */
    const signed char *q8; const float *scales; int ldb;    /* int8 table: one plane, its column scale */
    int store_first;                                        /* beta == 0: the first K block stores into C, no memset */
    atomic_int next_panel;                                  /* dynamic scheduling: the next 16-column panel to take */
};

static __m128i fv_clo16(const FiveValueB *fv, int col)
{
    signed char clo_b[16];
    for (int j = 0; j < NR; j++) {
        float lo = fv->lo[col + j], hi = fv->hi[col + j];
        int c = hi > 0.0f ? (int)lrintf(127.0f * lo / hi) : 0;
        clo_b[j] = (signed char)(c < 0 ? 0 : c > 127 ? 127 : c);
    }
    return _mm_loadu_si128((const __m128i *)clo_b);
}

/* As blas.c's NEON worker: threads take panels from a shared counter; within a panel K runs in KC_I8 blocks whose
 * packed rows stay in L1 while every A tile passes over them; each block's int32 sums are scaled and added into C. */
static void sgemm_i8_worker(int tid, int nthreads, void *aux)
{
    (void)tid; (void)nthreads;
    struct sgemm_i8_args *a = (struct sgemm_i8_args *)aux;
    int M = a->M, N = a->N, K = a->K, ldc = a->ldc;
    int tiles = (M + 3) / 4, Kq_total = K / 4;
    int panels_total = N / NR;

    size_t plane_bytes = (size_t)(KC_I8 / 4) * 64;
    I8Bufs *bufs = get_i8_bufs(0, 0, 2 * plane_bytes);
    if (!bufs) { fprintf(stderr, "sgemm_i8: out of memory\n"); abort(); }
    signed char *S = bufs->b, *T = bufs->b + plane_bytes;

    int p;
    while ((p = atomic_fetch_add(&a->next_panel, 1)) < panels_total) {
        int col = p * NR;

        /* Per-column epilogue factors: lo and hi − lo (two planes), hi / 127 (codes), or the int8 table's scale. */
        __m256 c1[2], c2[2] = { _mm256_setzero_ps(), _mm256_setzero_ps() };
        __m128i clo = _mm_setzero_si128();
        if (a->fv && a->codes) {
            clo = fv_clo16(a->fv, col);
            for (int j = 0; j < 2; j++)
                c1[j] = _mm256_mul_ps(_mm256_loadu_ps(a->fv->hi + col + 8 * j), _mm256_set1_ps(1.0f / 127.0f));
        } else if (a->fv) {
            for (int j = 0; j < 2; j++) {
                c1[j] = _mm256_loadu_ps(a->fv->lo + col + 8 * j);
                c2[j] = _mm256_sub_ps(_mm256_loadu_ps(a->fv->hi + col + 8 * j), c1[j]);
            }
        } else {
            for (int j = 0; j < 2; j++) c1[j] = _mm256_loadu_ps(a->scales + col + 8 * j);
        }
        int two_planes = a->fv && !a->codes;

        for (int kc = 0; kc < K; kc += KC_I8) {
            int kb = (kc + KC_I8 <= K) ? KC_I8 : (K - kc);
            int Kq = kb / 4;
            const signed char *Bp = S;
            if (a->fv && a->codes && a->fv->codes)
                Bp = a->fv->codes + ((size_t)p * K + kc) * NR;   /* pre-packed: nothing to expand */
            else if (a->fv && a->codes) pack_fv_codes_panel(a->fv, N, kc, kb, col, clo, S);
            else if (a->fv)             pack_fv_panel(a->fv, N, kc, kb, col, S, T);
            else                        pack_q8_panel(a->q8, a->ldb, kc, kb, col, S);

            for (int t = 0; t < tiles; t++) {
                const signed char *Ap = a->Ap + ((size_t)t * Kq_total + kc / 4) * 16;
                __m256i accS[4][2], accT[4][2];
                kernel_i8_4x16(Ap, Bp, Kq, accS);
                if (two_planes) kernel_i8_4x16(Ap, T, Kq, accT);
                int store = a->store_first && kc == 0;
                for (int r = 0; r < 4; r++) {
                    int m = 4 * t + r;
                    if (m >= M) break;
                    __m256 as = _mm256_set1_ps(a->a_scales[m] * a->alpha);
                    float *crow = a->C + (size_t)m * ldc + col;
                    for (int j = 0; j < 2; j++) {
                        __m256 f = _mm256_mul_ps(_mm256_cvtepi32_ps(accS[r][j]), c1[j]);
                        if (two_planes) f = _mm256_fmadd_ps(_mm256_cvtepi32_ps(accT[r][j]), c2[j], f);
                        if (store) _mm256_storeu_ps(crow + 8 * j, _mm256_mul_ps(f, as));
                        else       _mm256_storeu_ps(crow + 8 * j, _mm256_fmadd_ps(f, as, _mm256_loadu_ps(crow + 8 * j)));
                    }
                }
            }
        }
    }
}

struct pack_codes_args { const FiveValueB *b; int K, N; signed char *dst; atomic_int next; };

static void pack_codes_worker(int tid, int nthreads, void *aux)
{
    (void)tid; (void)nthreads;
    struct pack_codes_args *a = (struct pack_codes_args *)aux;
    int panels = a->N / NR, p;
    while ((p = atomic_fetch_add(&a->next, 1)) < panels) {
        int col = p * NR;
        pack_fv_codes_panel(a->b, a->N, 0, a->K, col, fv_clo16(a->b, col), a->dst + (size_t)p * a->K * NR);
    }
}

signed char *p2_fv_pack_codes(const FiveValueB *b, int K, int N)
{
    if (K % 4 != 0 || N % NR != 0) return NULL;
    signed char *codes = (signed char *)malloc((size_t)K * N);
    if (!codes) return NULL;
    struct pack_codes_args a = { b, K, N, codes, 0 };
    atomic_init(&a.next, 0);
    p2_parallel(p2_pool(), pack_codes_worker, &a);
    return codes;
}

/* The int8 GEMM on already quantised A: C = alpha · A · B + beta · C with beta ∈ {0, 1} (0 stores, 1 accumulates;
 * other values need scale_c first). */
static void sgemm_i8_run(int M, int N, int K, float alpha, const signed char *Ap, const float *a_scales,
                         const FiveValueB *fv, const signed char *q8, const float *scales, int ldb,
                         int beta_zero, float *C, int ldc)
{
    struct sgemm_i8_args args = { M, N, K, alpha, Ap, a_scales, C, ldc,
                                  fv, current_mode() == GEMM_INT8W, q8, scales, ldb, beta_zero, 0 };
    atomic_init(&args.next_panel, 0);
    p2_parallel(p2_pool(), sgemm_i8_worker, &args);
}

/* Quantise A on the pool, then run the int8 GEMM. Returns 0, or -1 when the shape is not supported. */
static int sgemm_i8(int M, int N, int K, float alpha, const float *A, int lda,
                    const FiveValueB *fv, const signed char *q8, const float *scales, int ldb,
                    int beta_zero, float *C, int ldc)
{
    if (K % 4 != 0) return -1;
    int Mp = (M + 3) / 4 * 4;
    I8Bufs *bufs = get_i8_bufs((size_t)Mp * K, Mp, 0);
    if (!bufs) return -1;

    struct quant_args qa = { A, M, K, lda, bufs->a, bufs->a_scales };
    p2_parallel(p2_pool(), quant_rows_worker, &qa);
    sgemm_i8_run(M, N, K, alpha, bufs->a, bufs->a_scales, fv, q8, scales, ldb, beta_zero, C, ldc);
    return 0;
}

void p2_sgemm_fvb_q8a(int M, int N, int K, float alpha, const QuantA *A, const FiveValueB *B,
                         float beta, float *C, int ldc)
{
    require_panel_aligned(N, "p2_sgemm_fvb_q8a");
    if (alpha == 0.0f) { scale_c(C, M, N, ldc, beta); return; }
    if (beta != 0.0f && beta != 1.0f) scale_c(C, M, N, ldc, beta);
    sgemm_i8_run(M, N, K, alpha, A->q, A->scales, B, NULL, NULL, 0, beta == 0.0f, C, ldc);
}

void p2_sgemm_q8b_q8a(int M, int N, int K, float alpha, const QuantA *A, const signed char *B, const float *scales,
                         int ldb, float beta, float *C, int ldc)
{
    require_panel_aligned(N, "p2_sgemm_q8b_q8a");
    if (alpha == 0.0f) { scale_c(C, M, N, ldc, beta); return; }
    if (beta != 0.0f && beta != 1.0f) scale_c(C, M, N, ldc, beta);
    sgemm_i8_run(M, N, K, alpha, A->q, A->scales, NULL, B, scales, ldb, beta == 0.0f, C, ldc);
}
