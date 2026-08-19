// Package codec is the format-agnostic serialization runtime that @codec
// codegen (gpp's noder.rewriteCodecDecorators) targets. One generated
// CodecEncode/CodecDecode method pair per struct works against any backend
// implementing Encoder/Decoder -- JSONEncoder/JSONDecoder and
// BinaryEncoder/BinaryDecoder ship here, and Codec adapts the binary
// backend to gRPC's encoding.Codec interface for @rpc services.
package codec

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

type Encodable interface{ CodecEncode(Encoder) error }
type Decodable interface{ CodecDecode(Decoder) error }

// Codec adapts the binary Encoder/Decoder backend to gRPC's
// encoding.Codec interface (Marshal/Unmarshal/Name), so a plain Go
// interface tagged @rpc gets real gRPC transport (HTTP/2, deadlines,
// interceptors, streaming) without protobuf or .proto files. Every @codec
// message type is automatically usable this way -- no extra codegen.
type Codec struct{}

func (Codec) Name() string { return "codec" }

func (Codec) Marshal(v any) ([]byte, error) {
	enc, ok := v.(Encodable)
	if !ok {
		return nil, fmt.Errorf("codec: %T does not implement Encodable (missing CodecEncode; did you forget @codec?)", v)
	}
	e := NewBinaryEncoder()
	if err := enc.CodecEncode(e); err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}

func (Codec) Unmarshal(data []byte, v any) error {
	dec, ok := v.(Decodable)
	if !ok {
		return fmt.Errorf("codec: %T does not implement Decodable (missing CodecDecode; did you forget @codec?)", v)
	}
	d := NewBinaryDecoder(data)
	return dec.CodecDecode(d)
}
