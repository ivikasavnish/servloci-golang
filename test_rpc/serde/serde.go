// Package serde is the format-agnostic serialization runtime that @serde
// codegen (gpp's noder.rewriteSerdeDecorators) targets. One generated
// SerdeEncode/SerdeDecode method pair per struct works against any backend
// implementing Encoder/Decoder -- JSONEncoder/JSONDecoder and
// BinaryEncoder/BinaryDecoder ship here, and SerdeCodec adapts the binary
// backend to gRPC's encoding.Codec interface for @rpc services.
package serde

import "fmt"

type Encoder interface {
	EncodeString(s string) error
	EncodeInt(i int64) error
	EncodeFloat(f float64) error
	EncodeBool(b bool) error
	EncodeOptional(present bool) error
	EncodeStructStart(name string, fieldNames []string) error
	EncodeStructEnd() error
	EncodeSeqStart(n int) error
	EncodeSeqEnd() error
	EncodeMapStart(n int) error
	EncodeMapEnd() error
}

type Decoder interface {
	DecodeString() (string, error)
	DecodeInt() (int64, error)
	DecodeFloat() (float64, error)
	DecodeBool() (bool, error)
	DecodeOptional() (bool, error)
	DecodeStructStart(name string, fieldNames []string) error
	DecodeStructEnd() error
	DecodeSeqStart() (int, error)
	DecodeSeqEnd() error
	DecodeMapStart() (int, error)
	DecodeMapEnd() error
}

type Encodable interface{ SerdeEncode(Encoder) error }
type Decodable interface{ SerdeDecode(Decoder) error }

// SerdeCodec adapts the binary Encoder/Decoder backend to gRPC's
// encoding.Codec interface (Marshal/Unmarshal/Name), so a plain Go
// interface tagged @rpc gets real gRPC transport (HTTP/2, deadlines,
// interceptors, streaming) without protobuf or .proto files. Every @serde
// message type is automatically usable this way -- no extra codegen.
type SerdeCodec struct{}

func (SerdeCodec) Name() string { return "serde" }

func (SerdeCodec) Marshal(v any) ([]byte, error) {
	enc, ok := v.(Encodable)
	if !ok {
		return nil, fmt.Errorf("serde: %T does not implement Encodable (missing SerdeEncode; did you forget @serde?)", v)
	}
	e := NewBinaryEncoder()
	if err := enc.SerdeEncode(e); err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}

func (SerdeCodec) Unmarshal(data []byte, v any) error {
	dec, ok := v.(Decodable)
	if !ok {
		return fmt.Errorf("serde: %T does not implement Decodable (missing SerdeDecode; did you forget @serde?)", v)
	}
	d := NewBinaryDecoder(data)
	return dec.SerdeDecode(d)
}
