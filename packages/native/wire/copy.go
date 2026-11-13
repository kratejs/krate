package wire

import "fmt"

// Copy reads exactly one value of any kind from src and writes it to dst.
// Blobs are re-indexed into dst's own blob list; Map and Set tags, escaped
// objects, dates and number text (including -0) come through unchanged, so
// copying a frame's value into a fresh Encoder reproduces the frame byte for
// byte. Copy walks generically, for hosts that handle values whose shape is
// only known at run time.
//
// On error dst may hold a partial value; discard it.
func Copy(dst *Encoder, src *Decoder) error {
	switch src.Kind() {
	case KindObject:
		if err := src.BeginObject(); err != nil {
			return err
		}
		dst.BeginObject()
		for src.More() {
			k, err := src.Key()
			if err != nil {
				return err
			}
			dst.Key(k)
			if err := Copy(dst, src); err != nil {
				return err
			}
		}
		dst.EndObject()
		return src.EndObject()
	case KindArray:
		if err := src.BeginArray(); err != nil {
			return err
		}
		dst.BeginArray()
		for src.More() {
			if err := Copy(dst, src); err != nil {
				return err
			}
		}
		dst.EndArray()
		return src.EndArray()
	case KindMap:
		if err := src.BeginMap(); err != nil {
			return err
		}
		dst.BeginMap()
		for src.More() {
			if err := Copy(dst, src); err != nil {
				return err
			}
		}
		dst.EndMap()
		return src.EndArray()
	case KindSet:
		if err := src.BeginSet(); err != nil {
			return err
		}
		dst.BeginSet()
		for src.More() {
			if err := Copy(dst, src); err != nil {
				return err
			}
		}
		dst.EndSet()
		return src.EndArray()
	case KindBytes:
		b, err := src.Bytes()
		if err != nil {
			return err
		}
		dst.Bytes(b)
		return nil
	case KindTime:
		t, err := src.Time()
		if err != nil {
			return err
		}
		dst.Time(t)
		return nil
	case KindString, KindNumber, KindBool, KindNull:
		raw, err := src.RawValue()
		if err != nil {
			return err
		}
		dst.Raw(raw)
		return nil
	}
	if err := src.Err(); err != nil {
		return err
	}
	return fmt.Errorf("wire: copy: unexpected value at offset %d", src.Offset())
}
