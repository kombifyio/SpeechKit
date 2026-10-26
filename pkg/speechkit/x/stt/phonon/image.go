package phonon

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

// This file is a Go port of convert() in convert_weights.py from
// eschmidbauer/phonon-2-c (MIT). It builds the three buffers that
// p2_engine_open_mem takes: the weight image, its JSON manifest and the token
// list. The output is byte-identical to the Python converter's weights.bin and
// tokens.txt, and its manifest has the same content.
//
// Layout of the image:
//
//   - five-value linears stay five values: a 2-bit sign plane and a 1-bit
//     lo/hi plane in [in, out] layout, with fp32 lo/hi per output column;
//     q_proj, k_proj and v_proj are concatenated into one [1024, 3072] tensor
//   - int6 tables become int8 with one fp32 scale per output feature. GEMM
//     tables (subsampling.linear, encoder_projector) are stored [in, out] with
//     the scale per column; decoder tables stay [out, in] with the scale per
//     row
//   - each depthwise convolution is widened to fp32 with its BatchNorm folded
//     in, the one step that rounds
//   - fp16 tensors become fp32
//
// Every tensor starts on a 64-byte boundary. The tensor names are the ones
// phonon-2-c's weights.c looks up.

const (
	// WeightsFormat is the "format" field of the manifest's _meta entry.
	WeightsFormat = "phonon-2-c-weights-v1"

	// DefaultBaseModel and DefaultLicense describe Phonon-2.5.
	DefaultBaseModel = "nvidia/parakeet-tdt-0.6b-v3"
	DefaultLicense   = "cc-by-4.0"

	converterName = "speechkit pkg/speechkit/x/stt/phonon"

	// VocabSize is the number of labels config.json carries; the image's token
	// list adds "<blank>".
	VocabSize = 8192

	imageAlign = 64
	bnEps      = 1e-5 // nn.BatchNorm1d default
	numBlocks  = 24
)

// Image is the in-memory input of p2_engine_open_mem.
type Image struct {
	// Bin is the weight image (weights.bin).
	Bin []byte
	// Manifest is the JSON tensor manifest (weights.json).
	Manifest []byte
	// Tokens is the newline-separated token list (tokens.txt), "<blank>" last.
	Tokens []byte
	// ContainerSHA256 is the SHA-256 of the model.fermion the image was built
	// from.
	ContainerSHA256 string
}

type meta struct {
	baseModel, license, wantSHA string
}

// BuildImage converts the container at containerPath into a weight image. The
// token list comes from the "labels" of the archive's config.json at
// configPath. It does not check the container against packed_manifest.json;
// [Load] does.
func BuildImage(containerPath, configPath string) (*Image, error) {
	return buildImage(containerPath, configPath, meta{baseModel: DefaultBaseModel, license: DefaultLicense}, nil)
}

// Allocator returns a zero-length slice with at least n bytes of capacity for
// the weight image, for a caller that wants the image outside the Go heap
// (the engine keeps it in C memory). The image never grows past it.
type Allocator func(n int) []byte

func buildImage(containerPath, configPath string, m meta, allocate Allocator) (*Image, error) {
	c, err := ReadContainer(containerPath)
	if err != nil {
		return nil, err
	}
	if m.wantSHA != "" && c.SHA256 != m.wantSHA {
		return nil, fmt.Errorf("phonon: %s has sha256 %s, but packed_manifest.json says %s", containerPath, c.SHA256, m.wantSHA)
	}
	tokens, err := readTokens(configPath)
	if err != nil {
		return nil, err
	}
	w := &imageWriter{c: c}
	if allocate != nil {
		// The image is about 1.44 times the records (five-value codes grow
		// from about 2.3 to 3 bits, fp16 to fp32); a larger image fails with
		// an error instead of growing.
		w.buf, w.fixed = allocate(c.size() * 8 / 5)[:0], true
	} else {
		w.buf = make([]byte, 0, c.size()*3/2)
	}
	w.convert()
	if w.err != nil {
		return nil, w.err
	}
	w.c = nil // the 178 MB container is garbage from here on
	w.entries = append(w.entries, entry{name: "_meta", meta: &metaEntry{
		format: WeightsFormat, sha: c.SHA256, records: len(c.Records),
		baseModel: m.baseModel, license: m.license, converter: converterName,
	}})
	return &Image{Bin: w.buf, Manifest: w.manifest(), Tokens: tokens, ContainerSHA256: c.SHA256}, nil
}

// size is the number of record bytes, a close lower bound of the image size.
func (c *Container) size() int {
	n := 0
	for _, r := range c.Records {
		n += len(r.blob)
	}
	return n
}

func readTokens(configPath string) ([]byte, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("phonon: read config: %w", err)
	}
	var cfg struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("phonon: config: %w", err)
	}
	if len(cfg.Labels) != VocabSize {
		return nil, fmt.Errorf("phonon: config has %d labels, want %d", len(cfg.Labels), VocabSize)
	}
	var out bytes.Buffer
	for _, l := range append(cfg.Labels, "<blank>") {
		if strings.Contains(l, "\n") {
			return nil, fmt.Errorf("phonon: label %q contains a newline", l)
		}
		out.WriteString(l)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

type metaEntry struct {
	format, sha, baseModel, license, converter string
	records                                    int
}

// entry is one manifest item; which offsets are meaningful depends on dtype.
type entry struct {
	name  string
	dtype string
	shape []int
	numel int
	// offset and scales for f32 and int8; signs, flags, lo and hi for
	// five_value.
	offset, scalesOffset, signsOffset, flagsOffset, loOffset, hiOffset int
	meta                                                               *metaEntry
}

// imageWriter appends tensors to the image. The first failure sticks in err
// and turns every later call into a no-op.
type imageWriter struct {
	c       *Container
	buf     []byte
	fixed   bool // buf came from an Allocator and must not be reallocated
	entries []entry
	scratch FiveValue // reused across five-value modules
	err     error
}

func (w *imageWriter) fail(format string, args ...any) {
	if w.err == nil {
		w.err = fmt.Errorf("phonon: "+format, args...)
	}
}

// alloc pads to the alignment, reserves n zeroed bytes and returns their
// offset. Slices into buf are valid only until the next alloc.
func (w *imageWriter) alloc(n int) int {
	pad := -len(w.buf) & (imageAlign - 1)
	if w.fixed && cap(w.buf)-len(w.buf) < pad+n {
		// Keep converting on the Go heap so the writes stay in bounds; the
		// error discards the result.
		w.fail("weight image exceeds its %d-byte buffer", cap(w.buf))
		w.fixed = false
	}
	w.buf = slices.Grow(w.buf, pad+n)
	start := len(w.buf)
	w.buf = w.buf[:start+pad+n]
	clear(w.buf[start:])
	return start + pad
}

func (w *imageWriter) record(name string) *Record {
	r, ok := w.c.Record(name)
	if !ok {
		w.fail("container has no record %q", name)
	}
	return r
}

// floats returns a record as float32 with its container shape.
func (w *imageWriter) floats(name string) ([]float32, []int) {
	r := w.record(name)
	if w.err != nil {
		return nil, nil
	}
	v, err := r.Float32()
	if err != nil {
		w.err = err
		return nil, nil
	}
	return v, r.Shape
}

func (w *imageWriter) put32(name string, shape []int, data []float32) {
	if w.err != nil {
		return
	}
	numel := 1
	for _, d := range shape {
		numel *= d
	}
	if numel != len(data) {
		w.fail("tensor %s has %d values, shape %v needs %d", name, len(data), shape, numel)
		return
	}
	off := w.alloc(4 * len(data))
	dst := w.buf[off:]
	for i, v := range data {
		binary.LittleEndian.PutUint32(dst[4*i:], math.Float32bits(v))
	}
	w.entries = append(w.entries, entry{name: name, dtype: "float32", shape: shape, numel: numel, offset: off})
}

// f32 stores a record as fp32 under name; shape overrides the container's
// when non-nil.
func (w *imageWriter) f32(name, from string, shape []int) {
	v, s := w.floats(from)
	if shape == nil {
		shape = s
	}
	w.put32(name, shape, v)
}

// i8 stores an int table as int8 rows x cols plus fp32 scales. When transpose
// is set the table is stored [in, out], one scale per column; otherwise it is
// kept [out, in], one scale per row.
func (w *imageWriter) i8(name, from string, transpose bool) {
	r := w.record(from)
	if w.err != nil {
		return
	}
	t, err := r.Int()
	if err != nil {
		w.err = err
		return
	}
	if t.Bits != 6 {
		w.fail("table %s has %d bits, want 6", from, t.Bits)
		return
	}
	rows, cols := t.Out, t.Cols
	if transpose {
		rows, cols = cols, rows
	}
	if cols%16 != 0 || (len(t.Scale) != rows && len(t.Scale) != cols) {
		w.fail("table %s [%d, %d] with %d scales does not fit the image layout", from, rows, cols, len(t.Scale))
		return
	}
	qOff := w.alloc(len(t.Q))
	sOff := w.alloc(4 * len(t.Scale))
	q, s := w.buf[qOff:qOff+len(t.Q)], w.buf[sOff:]
	if transpose {
		for o := range t.Out {
			for c := range t.Cols {
				q[c*t.Out+o] = byte(t.Q[o*t.Cols+c])
			}
		}
	} else {
		for i, v := range t.Q {
			q[i] = byte(v)
		}
	}
	for i, v := range t.Scale {
		binary.LittleEndian.PutUint32(s[4*i:], math.Float32bits(v))
	}
	w.entries = append(w.entries, entry{name: name, dtype: "int8", shape: []int{rows, cols}, offset: qOff, scalesOffset: sOff})
}

// fv stores the modules as one five-value tensor [K, N] in [in, out] layout,
// concatenated along N. Signs pack four per byte (weight n at bits 2*(n%4),
// code = sign+1), flags eight per byte (bit n%8), both along N.
func (w *imageWriter) fv(name string, modules ...string) {
	if w.err != nil {
		return
	}
	k, n := 0, 0
	recs := make([]*Record, len(modules))
	for i, m := range modules {
		recs[i] = w.record(m)
		if w.err != nil {
			return
		}
		if recs[i].Kind != KindFiveValue || len(recs[i].Shape) != 2 || (i > 0 && recs[i].Shape[1] != k) {
			w.fail("module %s cannot be concatenated into %s", m, name)
			return
		}
		k = recs[i].Shape[1]
		n += recs[i].Shape[0]
	}
	if n%16 != 0 {
		w.fail("tensor %s has %d outputs, want a multiple of 16", name, n)
		return
	}
	signsOff := w.alloc(k * (n / 4))
	flagsOff := w.alloc(k * (n / 8))
	loOff := w.alloc(4 * n)
	hiOff := w.alloc(4 * n)
	signs, flags := w.buf[signsOff:signsOff+k*(n/4)], w.buf[flagsOff:flagsOff+k*(n/8)]
	lo, hi := w.buf[loOff:loOff+4*n], w.buf[hiOff:hiOff+4*n]
	col := 0
	for _, r := range recs {
		fv, err := r.decodeFiveValue(&w.scratch)
		if err != nil {
			w.err = err
			return
		}
		for o := range fv.Out {
			c := col + o
			binary.LittleEndian.PutUint32(lo[4*c:], math.Float32bits(fv.Lo[o]))
			binary.LittleEndian.PutUint32(hi[4*c:], math.Float32bits(fv.Hi[o]))
			shift := uint(2 * (c % 4))
			bit := byte(1) << (c % 8)
			sb, fb := c/4, c/8
			row := fv.Sign[o*fv.In : (o+1)*fv.In]
			hiRow := fv.IsHi[o*fv.In : (o+1)*fv.In]
			for i, s := range row {
				signs[i*(n/4)+sb] |= byte(s+1) << shift
				if hiRow[i] {
					flags[i*(n/8)+fb] |= bit
				}
			}
		}
		col += fv.Out
	}
	w.entries = append(w.entries, entry{
		name: name, dtype: "five_value", shape: []int{k, n},
		signsOffset: signsOff, flagsOffset: flagsOff, loOffset: loOff, hiOffset: hiOff,
	})
}

// depthwise stores a depthwise convolution with its BatchNorm folded in:
// w' = w * g/sqrt(var+eps), b' = beta - mean * g/sqrt(var+eps), computed in
// float64 and rounded once to float32.
func (w *imageWriter) depthwise(name, p string) {
	wt, _ := w.floats(p + ".conv.depthwise_conv.weight")
	gamma, _ := w.floats(p + ".conv.norm.weight")
	beta, _ := w.floats(p + ".conv.norm.bias")
	mean, _ := w.floats(p + ".conv.norm.running_mean")
	variance, _ := w.floats(p + ".conv.norm.running_var")
	if w.err != nil {
		return
	}
	const ch, taps = 1024, 9
	if len(wt) != ch*taps || len(gamma) != ch || len(beta) != ch || len(mean) != ch || len(variance) != ch {
		w.fail("depthwise convolution %s has unexpected sizes", p)
		return
	}
	scaled := make([]float32, ch*taps)
	bias := make([]float32, ch)
	for c := range ch {
		s := float64(gamma[c]) / math.Sqrt(float64(variance[c])+bnEps)
		for t := range taps {
			scaled[c*taps+t] = float32(float64(wt[c*taps+t]) * s)
		}
		// The explicit conversion keeps the product rounded before the
		// subtraction on architectures that would fuse the two.
		bias[c] = float32(float64(beta[c]) - float64(float64(mean[c])*s))
	}
	w.put32(name+".weight", []int{ch, taps}, scaled)
	w.put32(name+".bias", []int{ch}, bias)
}

func (w *imageWriter) convert() {
	// Subsampling.
	sub := "encoder.subsampling"
	for _, layer := range []int{0, 2, 5} { // 3x3 convolutions: full, then two depthwise
		l := strconv.Itoa(layer)
		w.f32("subsampling.conv."+l+".weight", sub+".layers."+l+".weight", []int{256, 9})
		w.f32("subsampling.conv."+l+".bias", sub+".layers."+l+".bias", nil)
	}
	for _, layer := range []int{3, 6} { // pointwise 1x1 convolutions
		l := strconv.Itoa(layer)
		w.f32("subsampling.conv."+l+".weight", sub+".layers."+l+".weight", []int{256, 256})
		w.f32("subsampling.conv."+l+".bias", sub+".layers."+l+".bias", nil)
	}
	w.i8("subsampling.linear.weight", sub+".linear.weight", true)
	w.f32("subsampling.linear.bias", sub+".linear.bias", nil)

	// Conformer blocks.
	for i := range numBlocks {
		p, o := fmt.Sprintf("encoder.layers.%d", i), fmt.Sprintf("layers.%d", i)
		for _, norm := range []string{"norm_feed_forward1", "norm_self_att", "norm_conv", "norm_feed_forward2", "norm_out"} {
			w.f32(o+"."+norm+".weight", p+"."+norm+".weight", nil)
			w.f32(o+"."+norm+".bias", p+"."+norm+".bias", nil)
		}
		for _, ff := range []string{"feed_forward1", "feed_forward2"} {
			w.fv(o+"."+ff+".linear1.weight", p+"."+ff+".linear1")
			w.fv(o+"."+ff+".linear2.weight", p+"."+ff+".linear2")
		}
		w.fv(o+".self_attn.qkv_proj.weight", p+".self_attn.q_proj", p+".self_attn.k_proj", p+".self_attn.v_proj")
		w.fv(o+".self_attn.relative_k_proj.weight", p+".self_attn.relative_k_proj")
		w.fv(o+".self_attn.o_proj.weight", p+".self_attn.o_proj")
		w.f32(o+".self_attn.bias_u", p+".self_attn.bias_u", nil)
		w.f32(o+".self_attn.bias_v", p+".self_attn.bias_v", nil)
		w.fv(o+".conv.pointwise_conv1.weight", p+".conv.pointwise_conv1")
		w.fv(o+".conv.pointwise_conv2.weight", p+".conv.pointwise_conv2")
		w.depthwise(o+".conv.depthwise_conv", p)
	}

	w.i8("encoder_projector.weight", "encoder_projector.weight", true)
	w.f32("encoder_projector.bias", "encoder_projector.bias", nil)

	// Decoder: int8 rows, read row by row at decode time.
	w.checkBlankEmbedding()
	w.i8("decoder.embedding.weight", "decoder.embedding.weight", false)
	for layer := range 2 {
		l := strconv.Itoa(layer)
		w.i8("decoder.lstm.weight_ih_l"+l, "decoder.lstm.weight_ih_l"+l, false)
		w.i8("decoder.lstm.weight_hh_l"+l, "decoder.lstm.weight_hh_l"+l, false)
		w.f32("decoder.lstm.bias_ih_l"+l, "decoder.lstm.bias_ih_l"+l, nil)
		w.f32("decoder.lstm.bias_hh_l"+l, "decoder.lstm.bias_hh_l"+l, nil)
	}
	w.i8("decoder.decoder_projector.weight", "decoder.decoder_projector.weight", false)
	w.f32("decoder.decoder_projector.bias", "decoder.decoder_projector.bias", nil)
	w.i8("joint.head.weight", "joint.head.weight", false)
	w.f32("joint.head.bias", "joint.head.bias", nil)
}

// checkBlankEmbedding verifies the shape of the embedding table and that its
// blank row (the last one) is all zero, which the engine relies on.
func (w *imageWriter) checkBlankEmbedding() {
	r := w.record("decoder.embedding.weight")
	if w.err != nil {
		return
	}
	t, err := r.Int()
	if err != nil {
		w.err = err
		return
	}
	if t.Out != VocabSize+1 || t.Cols != 640 {
		w.fail("decoder embedding is [%d, %d], want [%d, 640]", t.Out, t.Cols, VocabSize+1)
		return
	}
	for _, q := range t.Q[VocabSize*t.Cols:] {
		if q != 0 {
			w.fail("blank embedding row is expected to be zero")
			return
		}
	}
}

// manifest renders the entries as the Python converter's json.dumps(indent=1)
// output followed by a newline.
func (w *imageWriter) manifest() []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, e := range w.entries {
		b.WriteString(" " + quote(e.name) + ": {\n")
		fields := e.fields()
		for j, f := range fields {
			b.WriteString("  " + quote(f.key) + ": " + f.value)
			if j < len(fields)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(" }")
		if i < len(w.entries)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes()
}

type field struct{ key, value string }

func (e entry) fields() []field {
	if m := e.meta; m != nil {
		return []field{
			{"format", quote(m.format)}, {"source_container_sha256", quote(m.sha)},
			{"source_records", strconv.Itoa(m.records)}, {"base_model", quote(m.baseModel)},
			{"license", quote(m.license)}, {"converter", quote(m.converter)},
		}
	}
	var shape strings.Builder
	shape.WriteString("[\n")
	for i, d := range e.shape {
		shape.WriteString("   " + strconv.Itoa(d))
		if i < len(e.shape)-1 {
			shape.WriteByte(',')
		}
		shape.WriteByte('\n')
	}
	shape.WriteString("  ]")
	head := []field{{"dtype", quote(e.dtype)}, {"shape", shape.String()}}
	switch e.dtype {
	case "float32":
		return append(head, field{"numel", strconv.Itoa(e.numel)}, field{"offset", strconv.Itoa(e.offset)})
	case "int8":
		return append(head, field{"offset", strconv.Itoa(e.offset)}, field{"scales_offset", strconv.Itoa(e.scalesOffset)})
	}
	return append(head,
		field{"signs_offset", strconv.Itoa(e.signsOffset)}, field{"flags_offset", strconv.Itoa(e.flagsOffset)},
		field{"lo_offset", strconv.Itoa(e.loOffset)}, field{"hi_offset", strconv.Itoa(e.hiOffset)})
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
