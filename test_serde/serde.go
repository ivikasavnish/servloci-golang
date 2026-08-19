package main

// Encoder/Decoder are the format-agnostic interfaces @serde-generated
// SerdeEncode/SerdeDecode methods talk to. One generated method pair per
// struct works with any backend implementing these -- JSON, a binary wire
// format, YAML, whatever -- without regenerating anything.
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
