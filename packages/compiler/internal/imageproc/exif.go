package imageproc

import (
	"encoding/binary"
	"image"
	"image/draw"
)

// jpegOrientation returns the EXIF orientation (1-8) of a JPEG file, or 1 when
// the file has no EXIF block or the tag is missing or invalid. The standard
// library decoder ignores EXIF, so the tag is read directly from the APP1
// segment.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == 0xD9 || marker == 0xDA { // end of image, start of scan
			return 1
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) { // no length
			i += 2
			continue
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		if marker == 0xE1 {
			seg := data[i+4 : i+2+size]
			if len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
				if o := tiffOrientation(seg[6:]); o != 0 {
					return o
				}
			}
		}
		i += 2 + size
	}
	return 1
}

// tiffOrientation reads the Orientation tag (0x0112) from the first IFD of an
// EXIF TIFF block. It returns 0 when the tag is absent or out of range.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(t[2:]) != 42 {
		return 0
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) != 0x0112 {
			continue
		}
		// type 3 (SHORT), count 1: the value sits in the first two bytes.
		if bo.Uint16(t[e+2:]) != 3 || bo.Uint32(t[e+4:]) != 1 {
			return 0
		}
		v := int(bo.Uint16(t[e+8:]))
		if v < 1 || v > 8 {
			return 0
		}
		return v
	}
	return 0
}

// applyOrientation returns img transformed so that it displays upright for the
// given EXIF orientation. Orientation 1 (or an unknown value) returns img
// unchanged. Orientations 5-8 swap the width and height.
func applyOrientation(img image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)

	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for sy := 0; sy < h; sy++ {
		for sx := 0; sx < w; sx++ {
			var dx, dy int
			switch orientation {
			case 2: // mirrored horizontally
				dx, dy = w-1-sx, sy
			case 3: // rotated 180
				dx, dy = w-1-sx, h-1-sy
			case 4: // mirrored vertically
				dx, dy = sx, h-1-sy
			case 5: // transposed
				dx, dy = sy, sx
			case 6: // rotated 90 clockwise
				dx, dy = h-1-sy, sx
			case 7: // transversed
				dx, dy = h-1-sy, w-1-sx
			case 8: // rotated 90 counter-clockwise
				dx, dy = sy, w-1-sx
			}
			so := src.PixOffset(sx, sy)
			do := dst.PixOffset(dx, dy)
			copy(dst.Pix[do:do+4], src.Pix[so:so+4])
		}
	}
	return dst
}
