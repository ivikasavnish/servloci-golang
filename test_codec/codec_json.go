package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// ---- JSONEncoder ----

type jsonFrame struct {
	kind      byte // 's' struct, 'q' seq, 'm' map
	n         int  // items (struct: fields written; seq: elems written; map: pairs written)
	fields    []string
	expectKey bool // map only
}

type JSONEncoder struct {
	buf   bytes.Buffer
	stack []*jsonFrame
}

func NewJSONEncoder() *JSONEncoder { return &JSONEncoder{} }
func (e *JSONEncoder) Bytes() []byte { return e.buf.Bytes() }
func (e *JSONEncoder) String() string { return e.buf.String() }

func (e *JSONEncoder) top() *jsonFrame {
	if len(e.stack) == 0 {
		return nil
	}
	return e.stack[len(e.stack)-1]
}

// beforeValue emits whatever punctuation (comma, "field":, or :) needs to
// precede the next scalar/container value, given the current container.
func (e *JSONEncoder) beforeValue() {
	f := e.top()
	if f == nil {
		return
	}
	switch f.kind {
	case 's':
		if f.n > 0 {
			e.buf.WriteByte(',')
		}
		writeJSONString(&e.buf, f.fields[f.n])
		e.buf.WriteByte(':')
		f.n++
	case 'q':
		if f.n > 0 {
			e.buf.WriteByte(',')
		}
		f.n++
	case 'm':
		if f.expectKey {
			if f.n > 0 {
				e.buf.WriteByte(',')
			}
			f.expectKey = false
		} else {
			e.buf.WriteByte(':')
			f.expectKey = true
			f.n++
		}
	}
}

func (e *JSONEncoder) EncodeString(s string) error {
	e.beforeValue()
	writeJSONString(&e.buf, s)
	return nil
}

func (e *JSONEncoder) EncodeInt(i int64) error {
	e.beforeValue()
	e.buf.WriteString(strconv.FormatInt(i, 10))
	return nil
}

func (e *JSONEncoder) EncodeFloat(f float64) error {
	e.beforeValue()
	e.buf.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
	return nil
}

func (e *JSONEncoder) EncodeBool(b bool) error {
	e.beforeValue()
	if b {
		e.buf.WriteString("true")
	} else {
		e.buf.WriteString("false")
	}
	return nil
}

func (e *JSONEncoder) EncodeOptional(present bool) error {
	if !present {
		e.beforeValue()
		e.buf.WriteString("null")
	}
	// if present, the caller immediately follows with the real value's
	// own Encode* call, which handles its own comma/field-name/colon.
	return nil
}

func (e *JSONEncoder) EncodeStructStart(name string, fieldNames []string) error {
	e.beforeValue()
	e.buf.WriteByte('{')
	e.stack = append(e.stack, &jsonFrame{kind: 's', fields: fieldNames})
	return nil
}

func (e *JSONEncoder) EncodeStructEnd() error {
	e.buf.WriteByte('}')
	e.stack = e.stack[:len(e.stack)-1]
	return nil
}

func (e *JSONEncoder) EncodeSeqStart(n int) error {
	e.beforeValue()
	e.buf.WriteByte('[')
	e.stack = append(e.stack, &jsonFrame{kind: 'q'})
	return nil
}

func (e *JSONEncoder) EncodeSeqEnd() error {
	e.buf.WriteByte(']')
	e.stack = e.stack[:len(e.stack)-1]
	return nil
}

func (e *JSONEncoder) EncodeMapStart(n int) error {
	e.beforeValue()
	e.buf.WriteByte('{')
	e.stack = append(e.stack, &jsonFrame{kind: 'm', expectKey: true})
	return nil
}

func (e *JSONEncoder) EncodeMapEnd() error {
	e.buf.WriteByte('}')
	e.stack = e.stack[:len(e.stack)-1]
	return nil
}

func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\t':
			buf.WriteString(`\t`)
		case '\r':
			buf.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// ---- JSONDecoder ----
//
// Parses the whole input into a generic value tree up front (map[string]any
// / []any / string / float64 / bool / nil), then Decode* calls walk that
// tree via a frame stack -- much simpler to get right than a streaming
// parser, and fine for the message sizes this is meant for.

type jsonDecFrame struct {
	kind   byte // 's' struct, 'q' seq, 'm' map
	fields []string
	idx    int
	seq    []any
	// map iteration, order doesn't matter for correctness
	mapKeys     []string
	mapVal      map[string]any
	expectValue bool
}

type JSONDecoder struct {
	stack []*jsonDecFrame
	next  any // the value about to be consumed by the next Decode* call
}

func NewJSONDecoder(data []byte) (*JSONDecoder, error) {
	v, rest, err := parseJSONValue(data)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("codec: trailing data after JSON value")
	}
	return &JSONDecoder{next: v}, nil
}

func (d *JSONDecoder) top() *jsonDecFrame {
	if len(d.stack) == 0 {
		return nil
	}
	return d.stack[len(d.stack)-1]
}

// take advances to the value the next Decode* call should consume,
// reading it out of the current container frame (if any).
func (d *JSONDecoder) take() (any, error) {
	f := d.top()
	if f == nil {
		v := d.next
		return v, nil
	}
	switch f.kind {
	case 's':
		if f.idx >= len(f.fields) {
			return nil, fmt.Errorf("codec: no more fields in struct")
		}
		v := f.mapVal[f.fields[f.idx]]
		f.idx++
		return v, nil
	case 'q':
		if f.idx >= len(f.seq) {
			return nil, fmt.Errorf("codec: sequence exhausted")
		}
		v := f.seq[f.idx]
		f.idx++
		return v, nil
	case 'm':
		// alternating key/value; key calls come through EncodeString-shaped
		// DecodeString, values through the element decoder.
		if f.idx >= len(f.mapKeys) {
			return nil, fmt.Errorf("codec: map exhausted")
		}
		if !f.expectValue {
			k := f.mapKeys[f.idx]
			f.expectValue = true
			return k, nil
		}
		k := f.mapKeys[f.idx]
		f.idx++
		f.expectValue = false
		return f.mapVal[k], nil
	}
	return nil, errors.New("codec: internal decoder state error")
}

func (d *JSONDecoder) DecodeString() (string, error) {
	v, err := d.take()
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("codec: expected string, got %T", v)
	}
	return s, nil
}

func (d *JSONDecoder) DecodeInt() (int64, error) {
	v, err := d.take()
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("codec: expected number, got %T", v)
	}
	return int64(f), nil
}

func (d *JSONDecoder) DecodeFloat() (float64, error) {
	v, err := d.take()
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("codec: expected number, got %T", v)
	}
	return f, nil
}

func (d *JSONDecoder) DecodeBool() (bool, error) {
	v, err := d.take()
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("codec: expected bool, got %T", v)
	}
	return b, nil
}

func (d *JSONDecoder) DecodeOptional() (bool, error) {
	v, err := d.take()
	if err != nil {
		return false, err
	}
	if v == nil {
		return false, nil
	}
	d.pushBack(v)
	return true, nil
}

// pushBack re-queues a value that DecodeOptional peeked at but didn't
// consume (since a present optional's real value still needs to be read
// by the following Decode* call). Implemented by rewinding the frame
// cursor we just advanced.
func (d *JSONDecoder) pushBack(v any) {
	f := d.top()
	if f == nil {
		d.next = v
		return
	}
	switch f.kind {
	case 's':
		f.idx--
	case 'q':
		f.idx--
	case 'm':
		f.expectValue = false
	}
}

func (d *JSONDecoder) DecodeStructStart(name string, fieldNames []string) error {
	v, err := d.take()
	if err != nil {
		return err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("codec: expected object for %s, got %T", name, v)
	}
	d.stack = append(d.stack, &jsonDecFrame{kind: 's', fields: fieldNames, mapVal: m})
	return nil
}

func (d *JSONDecoder) DecodeStructEnd() error {
	d.stack = d.stack[:len(d.stack)-1]
	return nil
}

func (d *JSONDecoder) DecodeSeqStart() (int, error) {
	v, err := d.take()
	if err != nil {
		return 0, err
	}
	s, ok := v.([]any)
	if !ok {
		return 0, fmt.Errorf("codec: expected array, got %T", v)
	}
	d.stack = append(d.stack, &jsonDecFrame{kind: 'q', seq: s})
	return len(s), nil
}

func (d *JSONDecoder) DecodeSeqEnd() error {
	d.stack = d.stack[:len(d.stack)-1]
	return nil
}

func (d *JSONDecoder) DecodeMapStart() (int, error) {
	v, err := d.take()
	if err != nil {
		return 0, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("codec: expected object, got %T", v)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	d.stack = append(d.stack, &jsonDecFrame{kind: 'm', mapKeys: keys, mapVal: m})
	return len(m), nil
}

func (d *JSONDecoder) DecodeMapEnd() error {
	d.stack = d.stack[:len(d.stack)-1]
	return nil
}

// ---- minimal JSON value parser ----

func parseJSONValue(b []byte) (any, []byte, error) {
	b = skipSpace(b)
	if len(b) == 0 {
		return nil, nil, errors.New("codec: unexpected end of JSON")
	}
	switch b[0] {
	case '{':
		return parseJSONObject(b)
	case '[':
		return parseJSONArray(b)
	case '"':
		return parseJSONStringLit(b)
	case 't':
		if len(b) >= 4 && string(b[:4]) == "true" {
			return true, b[4:], nil
		}
	case 'f':
		if len(b) >= 5 && string(b[:5]) == "false" {
			return false, b[5:], nil
		}
	case 'n':
		if len(b) >= 4 && string(b[:4]) == "null" {
			return nil, b[4:], nil
		}
	}
	return parseJSONNumber(b)
}

func skipSpace(b []byte) []byte {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return b[i:]
}

func parseJSONObject(b []byte) (any, []byte, error) {
	b = b[1:] // '{'
	m := map[string]any{}
	b = skipSpace(b)
	if len(b) > 0 && b[0] == '}' {
		return m, b[1:], nil
	}
	for {
		b = skipSpace(b)
		if len(b) == 0 || b[0] != '"' {
			return nil, nil, errors.New("codec: expected object key")
		}
		key, rest, err := parseJSONStringLit(b)
		if err != nil {
			return nil, nil, err
		}
		b = skipSpace(rest)
		if len(b) == 0 || b[0] != ':' {
			return nil, nil, errors.New("codec: expected ':' in object")
		}
		b = b[1:]
		val, rest2, err := parseJSONValue(b)
		if err != nil {
			return nil, nil, err
		}
		m[key.(string)] = val
		b = skipSpace(rest2)
		if len(b) == 0 {
			return nil, nil, errors.New("codec: unterminated object")
		}
		if b[0] == ',' {
			b = b[1:]
			continue
		}
		if b[0] == '}' {
			return m, b[1:], nil
		}
		return nil, nil, errors.New("codec: expected ',' or '}' in object")
	}
}

func parseJSONArray(b []byte) (any, []byte, error) {
	b = b[1:] // '['
	var arr []any
	b = skipSpace(b)
	if len(b) > 0 && b[0] == ']' {
		return []any{}, b[1:], nil
	}
	for {
		val, rest, err := parseJSONValue(b)
		if err != nil {
			return nil, nil, err
		}
		arr = append(arr, val)
		b = skipSpace(rest)
		if len(b) == 0 {
			return nil, nil, errors.New("codec: unterminated array")
		}
		if b[0] == ',' {
			b = b[1:]
			continue
		}
		if b[0] == ']' {
			return arr, b[1:], nil
		}
		return nil, nil, errors.New("codec: expected ',' or ']' in array")
	}
}

func parseJSONStringLit(b []byte) (any, []byte, error) {
	b = b[1:] // opening quote
	var out []byte
	for len(b) > 0 {
		c := b[0]
		if c == '"' {
			return string(out), b[1:], nil
		}
		if c == '\\' {
			if len(b) < 2 {
				return nil, nil, errors.New("codec: unterminated escape")
			}
			switch b[1] {
			case '"':
				out = append(out, '"')
			case '\\':
				out = append(out, '\\')
			case '/':
				out = append(out, '/')
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case 'r':
				out = append(out, '\r')
			case 'u':
				if len(b) < 6 {
					return nil, nil, errors.New("codec: bad \\u escape")
				}
				n, err := strconv.ParseUint(string(b[2:6]), 16, 32)
				if err != nil {
					return nil, nil, err
				}
				var tmp [utf8.UTFMax]byte
				w := utf8.EncodeRune(tmp[:], rune(n))
				out = append(out, tmp[:w]...)
				b = b[6:]
				continue
			default:
				return nil, nil, fmt.Errorf("codec: bad escape \\%c", b[1])
			}
			b = b[2:]
			continue
		}
		out = append(out, c)
		b = b[1:]
	}
	return nil, nil, errors.New("codec: unterminated string")
}

func parseJSONNumber(b []byte) (any, []byte, error) {
	i := 0
	for i < len(b) && (b[i] == '-' || b[i] == '+' || b[i] == '.' || b[i] == 'e' || b[i] == 'E' || (b[i] >= '0' && b[i] <= '9')) {
		i++
	}
	if i == 0 {
		return nil, nil, fmt.Errorf("codec: unexpected character %q", b[0])
	}
	f, err := strconv.ParseFloat(string(b[:i]), 64)
	if err != nil {
		return nil, nil, err
	}
	return f, b[i:], nil
}
