package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func frameOf(js string, blobs ...[]byte) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint32(b, uint32(len(js)))
	b = append(b, js...)
	b = binary.BigEndian.AppendUint16(b, uint16(len(blobs)))
	for _, x := range blobs {
		b = binary.BigEndian.AppendUint32(b, uint32(len(x)))
		b = append(b, x...)
	}
	return b
}

func dec(t *testing.T, js string, blobs ...[]byte) *Decoder {
	t.Helper()
	d, err := NewDecoder(frameOf(js, blobs...))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDepthLimit(t *testing.T) {
	js := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	d := dec(t, js)
	if err := d.Skip(); err == nil {
		t.Fatal("Skip must hit depth limit")
	}
	d = dec(t, js)
	var err error
	for i := 0; i < 600 && err == nil; i++ {
		err = d.BeginArray()
	}
	if err == nil {
		t.Fatal("BeginArray must hit depth limit")
	}
	d, _ = NewDecoder(frameOf(strings.Repeat("[", 10)+strings.Repeat("]", 10)), WithMaxDepth(5))
	if err := d.Skip(); err == nil {
		t.Fatal("custom depth limit")
	}
}

func TestReadFrameLimits(t *testing.T) {
	// A huge claimed length must be rejected before anything is allocated.
	hdr := []byte{0xff, 0xff, 0xff, 0xff}
	if _, err := ReadFrame(bytes.NewReader(hdr)); err == nil {
		t.Fatal("want limit error")
	}
	if _, err := ReadFrame(bytes.NewReader(hdr[:2])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short header: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(nil)); err != io.EOF {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(frameOf(`{}`)[:5])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated: %v", err)
	}
	f := frameOf(`{"a":1}`, make([]byte, 100))
	if _, err := ReadFrame(bytes.NewReader(f), WithMaxFrame(50)); err == nil {
		t.Fatal("blob over limit")
	}
	d, err := ReadFrame(bytes.NewReader(f))
	if err != nil || d.Blobs() != 1 {
		t.Fatal(err)
	}
}
