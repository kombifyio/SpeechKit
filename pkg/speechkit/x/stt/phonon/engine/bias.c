//go:build cgo && (amd64 || arm64)

/*  bias.c – Word boosting for the greedy TDT decoder (a SpeechKit addition, not upstream).
 *
 *  Dictionary terms ("SpeechKit", "voice typing") are split into SentencePiece pieces and stored in a trie of
 *  token ids. While decoding, a token that starts a term (from the trie root, at a word start) or continues a term
 *  the last tokens began gets `boost` added to its log-probability before the argmax. A token the model rates
 *  below P2_BIAS_FLOOR is never boosted, so the dictionary steers between close candidates instead of inserting
 *  words that were not said. Each term is stored as written and capitalised, and in
 *  every segmentation into pieces up to two pieces longer than the shortest, since the model need not emit the
 *  shortest one.
 */

#include "phonon2.h"
#include <ctype.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

#define P2_BIAS_FLOOR       (-12.0f)  /* log-probability below which a continuing token is never boosted */
#define P2_BIAS_START_BOOST 2.0f      /* a term's first piece gains less: a started term that is not said garbles the word */
#define P2_BIAS_START_FLOOR (-4.0f)
#define P2_BIAS_MAX_STATE 64        /* partial matches followed at once */
#define P2_BIAS_EXTRA     2         /* segmentations up to this many pieces longer than the shortest */
#define P2_BIAS_MAX_SEGS  32        /* segmentations kept per spelling */
#define P2_BIAS_MAX_BYTES 256       /* longest term, in bytes after ▁ marking */

typedef struct { int token, child, next; } BiasEdge;

struct P2Bias {
    int      *first;      /* per node: first edge, -1 for none */
    BiasEdge *edges;
    int       n_nodes, cap_nodes, n_edges, cap_edges;
    float     boost;
    float     floor;      /* log-probability below which a continuing token is never boosted */
    float     start_boost, start_floor;   /* the same for the first piece of a term, from the root */
    int       n_terms;
};

static int node_new(P2Bias *b)
{
    if (b->n_nodes >= b->cap_nodes) {
        int cap = b->cap_nodes ? b->cap_nodes * 2 : 256;
        int *p = (int *)realloc(b->first, (size_t)cap * sizeof(int));
        if (!p) return -1;
        b->first = p;
        b->cap_nodes = cap;
    }
    b->first[b->n_nodes] = -1;
    return b->n_nodes++;
}

static int child_of(const P2Bias *b, int node, int token)
{
    for (int e = b->first[node]; e >= 0; e = b->edges[e].next)
        if (b->edges[e].token == token) return b->edges[e].child;
    return -1;
}

static int child_add(P2Bias *b, int node, int token)
{
    int c = child_of(b, node, token);
    if (c >= 0) return c;
    c = node_new(b);
    if (c < 0) return -1;
    if (b->n_edges >= b->cap_edges) {
        int cap = b->cap_edges ? b->cap_edges * 2 : 256;
        BiasEdge *p = (BiasEdge *)realloc(b->edges, (size_t)cap * sizeof(BiasEdge));
        if (!p) return -1;
        b->edges = p;
        b->cap_edges = cap;
    }
    b->edges[b->n_edges] = (BiasEdge){ token, c, b->first[node] };
    b->first[node] = b->n_edges++;
    return c;
}

/* The term as the model writes it: "▁" before the first word and in place of every run of spaces. */
static int marked(const char *term, char *out, size_t cap)
{
    size_t n = 0;
    int space = 1;
    for (const unsigned char *p = (const unsigned char *)term; *p; p++) {
        if (isspace(*p)) { space = 1; continue; }
        if (space) {
            if (n + 3 >= cap) return -1;
            memcpy(out + n, "\xE2\x96\x81", 3);
            n += 3;
            space = 0;
        }
        if (n + 1 >= cap) return -1;
        out[n++] = (char)*p;
    }
    out[n] = '\0';
    return n > 3 ? (int)n : -1;   /* a term with no characters */
}

typedef struct {
    const P2Tokens *t;
    const char     *s;
    int             len;
    const int      *min_from;   /* fewest pieces covering s[i:] */
    int             limit;      /* most pieces allowed */
    int             path[P2_BIAS_MAX_BYTES];
    int             kept;
    P2Bias         *b;
} Segmenter;

/* Insert every segmentation of s[pos:] of at most `limit` pieces in total into the trie. */
static int segment_from(Segmenter *g, int pos, int depth)
{
    if (g->kept >= P2_BIAS_MAX_SEGS) return 0;
    if (pos == g->len) {
        int node = 0;
        for (int i = 0; i < depth; i++)
            if ((node = child_add(g->b, node, g->path[i])) < 0) return -1;
        g->kept++;
        return 0;
    }
    if (g->min_from[pos] < 0 || depth + g->min_from[pos] > g->limit) return 0;
    for (int id = 0; id < P2_BLANK; id++) {
        const char *piece = p2_tokens_piece(g->t, id);
        size_t plen = piece ? strlen(piece) : 0;
        if (plen == 0 || pos + (int)plen > g->len || memcmp(g->s + pos, piece, plen) != 0) continue;
        if (pos > 0 && plen >= 3 && memcmp(piece, "\xE2\x96\x81", 3) == 0 && memcmp(g->s + pos, "\xE2\x96\x81", 3) != 0)
            continue;
        g->path[depth] = id;
        if (segment_from(g, pos + (int)plen, depth + 1) != 0) return -1;
    }
    return 0;
}

/* Add one spelling of a term. Returns 0 (also when no segmentation exists), -1 on allocation failure. */
static int add_spelling(P2Bias *b, const P2Tokens *t, const char *term)
{
    char s[P2_BIAS_MAX_BYTES];
    int len = marked(term, s, sizeof(s));
    if (len < 0) return 0;
    int min_from[P2_BIAS_MAX_BYTES + 1];
    min_from[len] = 0;
    for (int pos = len - 1; pos >= 0; pos--) {
        min_from[pos] = -1;
        for (int id = 0; id < P2_BLANK; id++) {
            const char *piece = p2_tokens_piece(t, id);
            size_t plen = piece ? strlen(piece) : 0;
            if (plen == 0 || pos + (int)plen > len || min_from[pos + plen] < 0 ||
                memcmp(s + pos, piece, plen) != 0)
                continue;
            int n = 1 + min_from[pos + plen];
            if (min_from[pos] < 0 || n < min_from[pos]) min_from[pos] = n;
        }
    }
    if (min_from[0] < 0) return 0;
    Segmenter g = { t, s, len, min_from, min_from[0] + P2_BIAS_EXTRA, {0}, 0, b };
    return segment_from(&g, 0, 0);
}

P2Bias *p2_bias_new(const P2Tokens *t, const char *const *terms, int n_terms, float boost)
{
    if (!t || n_terms <= 0 || !terms || !(boost > 0.0f)) return NULL;
    P2Bias *b = (P2Bias *)calloc(1, sizeof(*b));
    if (!b || node_new(b) != 0) { p2_bias_free(b); return NULL; }
    b->boost = boost;
    b->floor = P2_BIAS_FLOOR;
    b->start_boost = P2_BIAS_START_BOOST;
    b->start_floor = P2_BIAS_START_FLOOR;
    /* tuning knobs, read once per dictionary */
    const char *env = getenv("P2_BIAS_BOOST");
    if (env && atof(env) > 0.0) b->boost = (float)atof(env);
    env = getenv("P2_BIAS_FLOOR");
    if (env && atof(env) < 0.0) b->floor = (float)atof(env);
    env = getenv("P2_BIAS_START_BOOST");
    if (env && atof(env) >= 0.0) b->start_boost = (float)atof(env);
    env = getenv("P2_BIAS_START_FLOOR");
    if (env && atof(env) < 0.0) b->start_floor = (float)atof(env);
    for (int i = 0; i < n_terms; i++) {
        const char *term = terms[i];
        if (!term || !*term || strlen(term) >= P2_BIAS_MAX_BYTES / 2) continue;
        char variant[P2_BIAS_MAX_BYTES];
        int nodes_before = b->n_nodes;
        if (add_spelling(b, t, term) != 0) { p2_bias_free(b); return NULL; }
        /* capitalised, for a term written in lower case at the start of a sentence; an all-lower-case spelling of
         * a name collides with ordinary words ("floating", "tool") and changed them on LibriSpeech */
        size_t len = strlen(term);
        memcpy(variant, term, len + 1);
        variant[0] = (char)toupper((unsigned char)variant[0]);
        if (strcmp(variant, term) != 0 && add_spelling(b, t, variant) != 0) { p2_bias_free(b); return NULL; }
        if (b->n_nodes > nodes_before) b->n_terms++;
    }
    if (b->n_terms == 0) { p2_bias_free(b); return NULL; }
    return b;
}

void p2_bias_free(P2Bias *b)
{
    if (!b) return;
    free(b->first);
    free(b->edges);
    free(b);
}

int p2_bias_terms(const P2Bias *b) { return b ? b->n_terms : 0; }

/* The token after boosting: the argmax over log-probabilities of the tokens, where every child of the root or of
 * an active partial match that the model rates at or above P2_BIAS_FLOOR gains b->boost. */
static int biased_argmax(const P2Bias *b, const int *active, int n_active, const float *logits)
{
    float max = logits[0];
    for (int i = 1; i < P2_VOCAB; i++) if (logits[i] > max) max = logits[i];
    double sum = 0.0;
    for (int i = 0; i < P2_VOCAB; i++) sum += exp((double)(logits[i] - max));
    float lse = max + (float)log(sum);

    int best = 0;
    float best_score = logits[0] - lse;
    for (int i = 1; i < P2_VOCAB; i++) {
        float lp = logits[i] - lse;
        if (lp > best_score) { best_score = lp; best = i; }
    }
    for (int a = -1; a < n_active; a++) {
        int node = a < 0 ? 0 : active[a];
        float boost = a < 0 ? b->start_boost : b->boost, floor = a < 0 ? b->start_floor : b->floor;
        for (int e = b->first[node]; e >= 0; e = b->edges[e].next) {
            int tok = b->edges[e].token;
            float lp = logits[tok] - lse;
            if (lp < floor) continue;
            if (lp + boost > best_score) { best_score = lp + boost; best = tok; }
        }
    }
    return best;
}

/* Advance the partial matches by an emitted token: every match the token continues, and a new one from the root. */
static int bias_advance(const P2Bias *b, int *active, int n_active, int token)
{
    int next[P2_BIAS_MAX_STATE], n = 0;
    for (int a = -1; a < n_active && n < P2_BIAS_MAX_STATE; a++) {
        int c = child_of(b, a < 0 ? 0 : active[a], token);
        if (c < 0) continue;
        int dup = 0;
        for (int k = 0; k < n && !dup; k++) dup = next[k] == c;
        if (!dup && b->first[c] >= 0) next[n++] = c;   /* a complete term with no longer one leaves nothing to follow */
    }
    memcpy(active, next, (size_t)n * sizeof(int));
    return n;
}

int p2_greedy_decode_bias(const P2Model *m, const float *enc_out, int T, const P2Bias *bias, P2Token **out_tokens)
{
    if (!bias) return p2_greedy_decode(m, enc_out, T, out_tokens);

    LSTMState state;
    memset(&state, 0, sizeof(state));
    int cap = T + 16;
    P2Token *tokens = (P2Token *)malloc(cap * sizeof(P2Token));
    int n_tokens = 0;
    float dec_out[P2_JOINT_DIM];
    float logits[P2_N_LOGITS];
    int active[P2_BIAS_MAX_STATE], n_active = 0;

    p2_decoder_step(m, P2_BLANK, &state, dec_out);

    int t = 0, symbols = 0;
    while (t < T) {
        p2_joint(m, enc_out + (size_t)t * P2_JOINT_DIM, dec_out, logits);
        int token = biased_argmax(bias, active, n_active, logits);
        int duration = p2_tdt_duration(logits + P2_VOCAB);

        if (token == P2_BLANK) {
            if (duration == 0) duration = 1;
        } else {
            if (n_tokens >= cap) {
                cap *= 2;
                tokens = (P2Token *)realloc(tokens, cap * sizeof(P2Token));
            }
            tokens[n_tokens].id = token;
            tokens[n_tokens].frame = t;
            tokens[n_tokens].duration = duration;
            n_tokens++;
            n_active = bias_advance(bias, active, n_active, token);
            p2_decoder_step(m, token, &state, dec_out);
            symbols++;
            if (duration == 0 && symbols >= P2_MAX_SYMBOLS) duration = 1;
        }
        if (duration) symbols = 0;
        t += duration;
    }

    *out_tokens = tokens;
    return n_tokens;
}
