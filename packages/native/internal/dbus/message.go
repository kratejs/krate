package dbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MessageType is the message type byte.
type MessageType byte

// Message types.
const (
	TypeMethodCall   MessageType = 1
	TypeMethodReturn MessageType = 2
	TypeError        MessageType = 3
	TypeSignal       MessageType = 4
)

// Flags.
const (
	FlagNoReplyExpected byte = 0x1
	FlagNoAutoStart     byte = 0x2
)

// Header field codes.
const (
	fieldPath        = 1
	fieldInterface   = 2
	fieldMember      = 3
	fieldErrorName   = 4
	fieldReplySerial = 5
	fieldDestination = 6
	fieldSender      = 7
	fieldSignature   = 8
	fieldUnixFDs     = 9
)

// maxMessage bounds one message; the D-Bus limit is 128 MiB.
const maxMessage = 64 << 20

// Message is one D-Bus message.
type Message struct {
	Type        MessageType
	Flags       byte
	Serial      uint32
	ReplySerial uint32
	Path        ObjectPath
	Interface   string
	Member      string
	ErrorName   string
	Destination string
	Sender      string
	// Signature is the body signature; Body holds the decoded values.
	Signature string
	Body      []any
}

// Marshal returns the wire form of m (little endian).
func (m *Message) Marshal() ([]byte, error) {
	body := &encoder{}
	if err := body.encodeAll(m.Signature, m.Body); err != nil {
		return nil, err
	}
	h := &encoder{}
	h.buf = append(h.buf, 'l', byte(m.Type), m.Flags, 1)
	h.u32(uint32(len(body.buf)))
	h.u32(m.Serial)

	type field struct {
		code byte
		sig  string
		val  any
	}
	var fields []field
	if m.Path != "" {
		fields = append(fields, field{fieldPath, "o", m.Path})
	}
	if m.Interface != "" {
		fields = append(fields, field{fieldInterface, "s", m.Interface})
	}
	if m.Member != "" {
		fields = append(fields, field{fieldMember, "s", m.Member})
	}
	if m.ErrorName != "" {
		fields = append(fields, field{fieldErrorName, "s", m.ErrorName})
	}
	if m.Type == TypeMethodReturn || m.Type == TypeError {
		fields = append(fields, field{fieldReplySerial, "u", m.ReplySerial})
	}
	if m.Destination != "" {
		fields = append(fields, field{fieldDestination, "s", m.Destination})
	}
	if m.Sender != "" {
		fields = append(fields, field{fieldSender, "s", m.Sender})
	}
	if m.Signature != "" {
		fields = append(fields, field{fieldSignature, "g", Signature(m.Signature)})
	}
	h.u32(0)
	lenAt := len(h.buf) - 4
	h.align(8)
	start := len(h.buf)
	for _, f := range fields {
		h.align(8)
		h.buf = append(h.buf, f.code)
		if err := h.sig(f.sig); err != nil {
			return nil, err
		}
		if err := h.encode(f.sig, f.val, 1); err != nil {
			return nil, err
		}
	}
	binary.LittleEndian.PutUint32(h.buf[lenAt:], uint32(len(h.buf)-start))
	h.align(8)
	out := append(h.buf, body.buf...)
	if len(out) > maxMessage {
		return nil, errors.New("dbus: message too large")
	}
	return out, nil
}

// ReadMessage reads and decodes one message.
func ReadMessage(r io.Reader) (*Message, error) {
	fixed := make([]byte, 16)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return nil, err
	}
	var ord binary.ByteOrder
	switch fixed[0] {
	case 'l':
		ord = binary.LittleEndian
	case 'B':
		ord = binary.BigEndian
	default:
		return nil, fmt.Errorf("dbus: bad byte order %q", fixed[0])
	}
	if fixed[3] != 1 {
		return nil, fmt.Errorf("dbus: unsupported protocol version %d", fixed[3])
	}
	bodyLen := int(ord.Uint32(fixed[4:8]))
	fieldsLen := int(ord.Uint32(fixed[12:16]))
	headerEnd := (16 + fieldsLen + 7) &^ 7
	if fieldsLen > maxMessage || bodyLen > maxMessage || headerEnd+bodyLen > maxMessage {
		return nil, errors.New("dbus: message too large")
	}
	buf := make([]byte, headerEnd+bodyLen)
	copy(buf, fixed)
	if _, err := io.ReadFull(r, buf[16:]); err != nil {
		return nil, err
	}
	m := &Message{Type: MessageType(fixed[1]), Flags: fixed[2], Serial: ord.Uint32(fixed[8:12])}
	d := &decoder{buf: buf[:headerEnd], pos: 16, ord: ord}
	for d.pos < 16+fieldsLen {
		if err := d.align(8); err != nil {
			return nil, err
		}
		code, err := d.take(1)
		if err != nil {
			return nil, err
		}
		sig, err := d.sig()
		if err != nil {
			return nil, err
		}
		if n, terr := typeLen(sig); terr != nil || n != len(sig) {
			return nil, fmt.Errorf("dbus: bad header field signature %q", sig)
		}
		v, err := d.decode(sig, 1)
		if err != nil {
			return nil, err
		}
		switch code[0] {
		case fieldPath:
			m.Path, _ = v.(ObjectPath)
		case fieldInterface:
			m.Interface, _ = v.(string)
		case fieldMember:
			m.Member, _ = v.(string)
		case fieldErrorName:
			m.ErrorName, _ = v.(string)
		case fieldReplySerial:
			m.ReplySerial, _ = v.(uint32)
		case fieldDestination:
			m.Destination, _ = v.(string)
		case fieldSender:
			m.Sender, _ = v.(string)
		case fieldSignature:
			s, _ := v.(Signature)
			m.Signature = string(s)
		}
	}
	if bodyLen > 0 {
		// The body starts 8-aligned, so it is decoded on its own.
		bd := &decoder{buf: buf[headerEnd:], ord: ord}
		body, err := bd.decodeAll(m.Signature)
		if err != nil {
			return nil, err
		}
		m.Body = body
	}
	return m, nil
}

// Error is a D-Bus error reply. Name is like
// "org.freedesktop.DBus.Error.UnknownMethod"; Body carries its arguments,
// usually one message string.
type Error struct {
	Name string
	Body []any
}

// Error implements error.
func (e *Error) Error() string {
	if len(e.Body) > 0 {
		if s, ok := e.Body[0].(string); ok && s != "" {
			return e.Name + ": " + s
		}
	}
	return e.Name
}

// NewError builds an *Error with a message.
func NewError(name, msg string) *Error {
	return &Error{Name: name, Body: []any{msg}}
}
