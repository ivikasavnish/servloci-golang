package serde

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// BinaryEncoder is a compact, length-prefixed, positional wire format:
// field/map key names are never written (both sides are compiled from the
// same struct, so declaration order is the schema). Every @serde struct
// gets this backend for free -- no extra codegen.
type BinaryEncoder struct {
	buf bytes.Buffer
}

func NewBinaryEncoder() *BinaryEncoder  { return &BinaryEncoder{} }
func (e *BinaryEncoder) Bytes() []byte  { return e.buf.Bytes() }

func (e *BinaryEncoder) putUvarint(n uint64) {
	var tmp [binary.MaxVarintLen64]byte
	k := binary.PutUvarint(tmp[:], n)
	e.buf.Write(tmp[:k])
}

func (e *BinaryEncoder) EncodeString(s string) error {
	e.putUvarint(uint64(len(s)))
	e.buf.WriteString(s)
	return nil
}

func (e *BinaryEncoder) EncodeInt(i int64) error {
	var tmp [binary.MaxVarintLen64]byte
	k := binary.PutVarint(tmp[:], i)
	e.buf.Write(tmp[:k])
	return nil
}

func (e *BinaryEncoder) EncodeFloat(f float64) error {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(f))
	e.buf.Write(tmp[:])
	return nil
}

func (e *BinaryEncoder) EncodeBool(b bool) error {
	if b {
		e.buf.WriteByte(1)
	} else {
		e.buf.WriteByte(0)
	}
	return nil
}

func (e *BinaryEncoder) EncodeOptional(present bool) error { return e.EncodeBool(present) }

func (e *BinaryEncoder) EncodeStructStart(name string, fieldNames []string) error { return nil }
func (e *BinaryEncoder) EncodeStructEnd() error                                   { return nil }

func (e *BinaryEncoder) EncodeSeqStart(n int) error { e.putUvarint(uint64(n)); return nil }
func (e *BinaryEncoder) EncodeSeqEnd() error        { return nil }

func (e *BinaryEncoder) EncodeMapStart(n int) error { e.putUvarint(uint64(n)); return nil }
func (e *BinaryEncoder) EncodeMapEnd() error        { return nil }

// BinaryDecoder mirrors BinaryEncoder.
type BinaryDecoder struct {
	data []byte
	pos  int
}

func NewBinaryDecoder(data []byte) *BinaryDecoder { return &BinaryDecoder{data: data} }

func (d *BinaryDecoder) getUvarint() (uint64, error) {
	n, k := binary.Uvarint(d.data[d.pos:])
	if k <= 0 {
		return 0, errors.New("serde: bad varint")
	}
	d.pos += k
	return n, nil
}

func (d *BinaryDecoder) DecodeString() (string, error) {
	n, err := d.getUvarint()
	if err != nil {
		return "", err
	}
	if d.pos+int(n) > len(d.data) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(d.data[d.pos : d.pos+int(n)])
	d.pos += int(n)
	return s, nil
}

func (d *BinaryDecoder) DecodeInt() (int64, error) {
	n, k := binary.Varint(d.data[d.pos:])
	if k <= 0 {
		return 0, errors.New("serde: bad varint")
	}
	d.pos += k
	return n, nil
}

func (d *BinaryDecoder) DecodeFloat() (float64, error) {
	if d.pos+8 > len(d.data) {
		return 0, io.ErrUnexpectedEOF
	}
	bits := binary.LittleEndian.Uint64(d.data[d.pos : d.pos+8])
	d.pos += 8
	return math.Float64frombits(bits), nil
}

func (d *BinaryDecoder) DecodeBool() (bool, error) {
	if d.pos >= len(d.data) {
		return false, io.ErrUnexpectedEOF
	}
	b := d.data[d.pos] != 0
	d.pos++
	return b, nil
}

func (d *BinaryDecoder) DecodeOptional() (bool, error) { return d.DecodeBool() }

func (d *BinaryDecoder) DecodeStructStart(name string, fieldNames []string) error { return nil }
func (d *BinaryDecoder) DecodeStructEnd() error                                   { return nil }

func (d *BinaryDecoder) DecodeSeqStart() (int, error) {
	n, err := d.getUvarint()
	return int(n), err
}
func (d *BinaryDecoder) DecodeSeqEnd() error { return nil }

func (d *BinaryDecoder) DecodeMapStart() (int, error) {
	n, err := d.getUvarint()
	return int(n), err
}
func (d *BinaryDecoder) DecodeMapEnd() error { return nil }
