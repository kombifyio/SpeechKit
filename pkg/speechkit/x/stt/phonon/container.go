package phonon

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
)

// This file is a Go port of fermion_container.py from Fermion Research's
// Phonon-2.5 release (Apache-2.0). It reads the
// "fermion-five-value-parakeet-v1" container and keeps each record in its raw
// quantized form, which the weight image builder needs.

// ContainerFormat is the only container format the reader accepts.
const ContainerFormat = "fermion-five-value-parakeet-v1"

// Record kinds in a container index.
const (
	KindFiveValue = "five_value"
	KindInt6      = "int6"
	KindInt8      = "int8"
	KindFP16      = "fp16"
)

// maxHeaderBytes bounds the JSON header; the real ones are about 100 KB.
const maxHeaderBytes = 64 << 20

// maxElements bounds the element count of one record; it fits int on
// 32-bit platforms too.
const maxElements = math.MaxInt32

// Container is a parsed model.fermion. It holds the whole file in memory and
// its records alias that memory, so a Container must not be modified.
type Container struct {
	// SHA256 is the lowercase hex SHA-256 of the file.
	SHA256  string
	Records []Record
	byName  map[string]int
}

// Record is one tensor of a container, still encoded.
type Record struct {
	// Name is the HF Transformers (ParakeetForTDT) name. For a five-value
	// record it is the module name, without the ".weight" suffix.
	Name  string
	Kind  string
	Shape []int
	blob  []byte
}

// FiveValue is a decoded five-value linear module. Out and In are the
// module's output and input features; weight (o, i) is
// Sign * (IsHi ? Hi[o] : Lo[o]).
type FiveValue struct {
	Out, In int
	// Sign is [Out*In], row-major, each value -1, 0 or 1.
	Sign []int8
	// IsHi is [Out*In], row-major. It is only set where Sign is non-zero.
	IsHi []bool
	// Lo and Hi are the per-output magnitudes, exact fp16 values.
	Lo, Hi []float32
}

// IntTable is a decoded int6 or int8 table. Weight (o, c) is
// Q[o*Cols+c] * Scale[o].
type IntTable struct {
	Out, Cols int
	Bits      int
	// Q is [Out*Cols], row-major; int6 codes are already offset to -32..31.
	Q []int8
	// Scale is the per-row scale, an exact fp16 value.
	Scale []float32
}

type containerHeader struct {
	Format string        `json:"format"`
	Index  []indexRecord `json:"index"`
}

type indexRecord struct {
	N     string `json:"n"`
	K     string `json:"k"`
	Shape []int  `json:"shape"`
	B     int    `json:"b"`
}

// ReadContainer reads and validates the container at path. It fails closed on
// an unknown format or kind, a truncated record and trailing bytes.
func ReadContainer(path string) (*Container, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("phonon: read container: %w", err)
	}
	return parseContainer(data)
}

func parseContainer(data []byte) (*Container, error) {
	if len(data) < 8 {
		return nil, errors.New("phonon: container is shorter than its header length")
	}
	n := binary.LittleEndian.Uint64(data)
	if n > maxHeaderBytes || n > uint64(len(data)-8) {
		return nil, fmt.Errorf("phonon: container header length %d is invalid", n)
	}
	var header containerHeader
	if err := json.Unmarshal(data[8:8+n], &header); err != nil {
		return nil, fmt.Errorf("phonon: container header: %w", err)
	}
	if header.Format != ContainerFormat {
		return nil, fmt.Errorf("phonon: container format %q, want %q", header.Format, ContainerFormat)
	}
	sum := sha256.Sum256(data)
	c := &Container{
		SHA256:  hex.EncodeToString(sum[:]),
		Records: make([]Record, 0, len(header.Index)),
		byName:  make(map[string]int, len(header.Index)),
	}
	rest := data[8+n:]
	for _, e := range header.Index {
		switch e.K {
		case KindFiveValue, KindInt6, KindInt8, KindFP16:
		default:
			return nil, fmt.Errorf("phonon: record %q has unknown kind %q", e.N, e.K)
		}
		if _, dup := c.byName[e.N]; dup {
			return nil, fmt.Errorf("phonon: duplicate record %q", e.N)
		}
		if e.B < 0 || e.B > len(rest) {
			return nil, fmt.Errorf("phonon: record %q is truncated", e.N)
		}
		if _, err := elements(e.N, e.Shape); err != nil {
			return nil, err
		}
		c.byName[e.N] = len(c.Records)
		c.Records = append(c.Records, Record{Name: e.N, Kind: e.K, Shape: e.Shape, blob: rest[:e.B:e.B]})
		rest = rest[e.B:]
	}
	if len(rest) != 0 {
		return nil, errors.New("phonon: container has trailing bytes")
	}
	return c, nil
}

// Record returns the record called name.
func (c *Container) Record(name string) (*Record, bool) {
	i, ok := c.byName[name]
	if !ok {
		return nil, false
	}
	return &c.Records[i], true
}

func elements(name string, shape []int) (int, error) {
	total := 1
	for _, d := range shape {
		if d <= 0 || total > maxElements/d {
			return 0, fmt.Errorf("phonon: record %q has invalid shape %v", name, shape)
		}
		total *= d
	}
	return total, nil
}

// tritTable[b] holds the five base-3 digits of byte b, least significant
// first, as fermion_container.py's (b // 3^k) % 3.
var tritTable = func() (t [256][5]uint8) {
	for b := range t {
		for k, p := 0, 1; k < 5; k, p = k+1, p*3 {
			t[b][k] = uint8(b / p % 3)
		}
	}
	return t
}()

// FiveValue decodes a five-value record: trit-packed signs (5 per byte), one
// is_hi bit per non-zero sign in a little-endian bit stream, then fp16 lo and
// hi per output. The record must have exactly the expected length.
func (r *Record) FiveValue() (*FiveValue, error) {
	return r.decodeFiveValue(new(FiveValue))
}

// decodeFiveValue is FiveValue decoding into fv, reusing its buffers.
func (r *Record) decodeFiveValue(fv *FiveValue) (*FiveValue, error) {
	if r.Kind != KindFiveValue {
		return nil, fmt.Errorf("phonon: record %q is %s, not %s", r.Name, r.Kind, KindFiveValue)
	}
	if len(r.Shape) != 2 {
		return nil, fmt.Errorf("phonon: five-value record %q has shape %v, want 2 dimensions", r.Name, r.Shape)
	}
	o, in := r.Shape[0], r.Shape[1]
	rowBytes := (in + 4) / 5
	signBytes := o * rowBytes
	if signBytes > len(r.blob) {
		return nil, fmt.Errorf("phonon: five-value record %q is truncated", r.Name)
	}
	fv.Out, fv.In = o, in
	fv.Sign = slices.Grow(fv.Sign[:0], o*in)[:o*in]
	fv.IsHi = slices.Grow(fv.IsHi[:0], o*in)[:o*in]
	clear(fv.IsHi)
	nnz := 0
	for row := range o {
		src := r.blob[row*rowBytes : (row+1)*rowBytes]
		dst := fv.Sign[row*in : (row+1)*in]
		for col := range dst {
			code := tritTable[src[col/5]][col%5]
			dst[col] = int8(code) - 1
			if code != 1 {
				nnz++
			}
		}
	}
	bitBytes := (nnz + 7) / 8
	if signBytes+bitBytes+4*o != len(r.blob) {
		return nil, fmt.Errorf("phonon: five-value record %q has %d bytes, want %d", r.Name, len(r.blob), signBytes+bitBytes+4*o)
	}
	bits := r.blob[signBytes : signBytes+bitBytes]
	n := 0
	for i, s := range fv.Sign {
		if s != 0 {
			fv.IsHi[i] = bits[n>>3]>>(n&7)&1 == 1
			n++
		}
	}
	scales := r.blob[signBytes+bitBytes:]
	fv.Lo = halves(scales[:2*o])
	fv.Hi = halves(scales[2*o:])
	return fv, nil
}

// Int decodes an int6 or int8 record: the codes followed by one fp16 scale
// per row. An int6 body packs four codes into three bytes, each stored as
// value+32.
func (r *Record) Int() (*IntTable, error) {
	bits := 0
	switch r.Kind {
	case KindInt6:
		bits = 6
	case KindInt8:
		bits = 8
	default:
		return nil, fmt.Errorf("phonon: record %q is %s, not an int table", r.Name, r.Kind)
	}
	if len(r.Shape) == 0 {
		return nil, fmt.Errorf("phonon: int record %q has no dimensions", r.Name)
	}
	total, _ := elements(r.Name, r.Shape)
	o := r.Shape[0]
	if total%o != 0 {
		return nil, fmt.Errorf("phonon: int record %q has an invalid shape %v", r.Name, r.Shape)
	}
	if len(r.blob) < 2*o {
		return nil, fmt.Errorf("phonon: int record %q is truncated", r.Name)
	}
	body := r.blob[:len(r.blob)-2*o]
	t := &IntTable{Out: o, Cols: total / o, Bits: bits, Q: make([]int8, total), Scale: halves(r.blob[len(r.blob)-2*o:])}
	if bits == 8 {
		if len(body) != total {
			return nil, fmt.Errorf("phonon: int8 record %q has %d code bytes, want %d", r.Name, len(body), total)
		}
		for i, b := range body {
			t.Q[i] = int8(b)
		}
		return t, nil
	}
	if want := (total + 3) / 4 * 3; len(body) != want {
		return nil, fmt.Errorf("phonon: int6 record %q has %d code bytes, want %d", r.Name, len(body), want)
	}
	for g := 0; g < len(body)/3; g++ {
		packed := uint32(body[3*g]) | uint32(body[3*g+1])<<8 | uint32(body[3*g+2])<<16
		for k := 0; k < 4 && 4*g+k < total; k++ {
			t.Q[4*g+k] = int8(int(packed>>(6*k)&0x3f) - 32)
		}
	}
	return t, nil
}

// Dequantize returns Q*Scale per row in float32, the way the reference reader
// does.
func (t *IntTable) Dequantize() []float32 {
	out := make([]float32, len(t.Q))
	for o := range t.Out {
		s := t.Scale[o]
		row := t.Q[o*t.Cols : (o+1)*t.Cols]
		dst := out[o*t.Cols : (o+1)*t.Cols]
		for c, q := range row {
			dst[c] = float32(q) * s
		}
	}
	return out
}

// Float32 returns the values of a record as float32, row-major: fp16 records
// are widened and int records dequantized. Five-value records are refused;
// use [Record.FiveValue].
func (r *Record) Float32() ([]float32, error) {
	switch r.Kind {
	case KindFP16:
		total, _ := elements(r.Name, r.Shape)
		if len(r.blob) != 2*total {
			return nil, fmt.Errorf("phonon: fp16 record %q has %d bytes, want %d", r.Name, len(r.blob), 2*total)
		}
		return halves(r.blob), nil
	case KindInt6, KindInt8:
		t, err := r.Int()
		if err != nil {
			return nil, err
		}
		return t.Dequantize(), nil
	}
	return nil, fmt.Errorf("phonon: record %q is %s and has no plain float form", r.Name, r.Kind)
}

// halves widens little-endian IEEE 754 binary16 values; the conversion is
// exact.
func halves(b []byte) []float32 {
	out := make([]float32, len(b)/2)
	for i := range out {
		out[i] = halfToFloat32(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

func halfToFloat32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff
	switch exp {
	case 0:
		// Zero or subnormal: frac * 2^-24, exact in float32.
		return math.Float32frombits(math.Float32bits(float32(frac)*(1.0/16777216)) | sign)
	case 31:
		return math.Float32frombits(sign | 0x7f800000 | frac<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | frac<<13)
}
