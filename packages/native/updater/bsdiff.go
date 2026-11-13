package updater

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// This file is a pure Go implementation of the bsdiff/bspatch algorithm
// (Colin Percival): a suffix-array search finds long approximate matches and
// the patch stores the byte-wise difference of the matched regions, which
// compresses to almost nothing for a recompiled executable whose code moved a
// little. The container is Krate's own (control, diff and extra streams
// deflated together), not the BSDIFF40 file format.

// maxDiffBase bounds the size of a file bsdiff works on: the suffix array
// takes 8 bytes per input byte.
const maxDiffBase = 256 << 20

// bsSplit and bsSort are the Larsson-Sadakane suffix sort used by bsdiff.
func bsSplit(I, V []int32, start, length, h int32) {
	if length < 16 {
		for k := start; k < start+length; {
			j := int32(1)
			x := V[I[k]+h]
			for i := int32(1); k+i < start+length; i++ {
				if V[I[k+i]+h] < x {
					x = V[I[k+i]+h]
					j = 0
				}
				if V[I[k+i]+h] == x {
					I[k+j], I[k+i] = I[k+i], I[k+j]
					j++
				}
			}
			for i := int32(0); i < j; i++ {
				V[I[k+i]] = k + j - 1
			}
			if j == 1 {
				I[k] = -1
			}
			k += j
		}
		return
	}
	x := V[I[start+length/2]+h]
	var jj, kk int32
	for i := start; i < start+length; i++ {
		if V[I[i]+h] < x {
			jj++
		}
		if V[I[i]+h] == x {
			kk++
		}
	}
	jj += start
	kk += jj
	i, j, k := start, int32(0), int32(0)
	for i < jj {
		switch {
		case V[I[i]+h] < x:
			i++
		case V[I[i]+h] == x:
			I[i], I[jj+j] = I[jj+j], I[i]
			j++
		default:
			I[i], I[kk+k] = I[kk+k], I[i]
			k++
		}
	}
	for jj+j < kk {
		if V[I[jj+j]+h] == x {
			j++
		} else {
			I[jj+j], I[kk+k] = I[kk+k], I[jj+j]
			k++
		}
	}
	if jj > start {
		bsSplit(I, V, start, jj-start, h)
	}
	for i := int32(0); i < kk-jj; i++ {
		V[I[jj+i]] = kk - 1
	}
	if jj == kk-1 {
		I[jj] = -1
	}
	if start+length > kk {
		bsSplit(I, V, kk, start+length-kk, h)
	}
}

func bsSuffixSort(old []byte) []int32 {
	n := int32(len(old))
	I := make([]int32, n+1)
	V := make([]int32, n+1)
	var buckets [256]int32
	for _, c := range old {
		buckets[c]++
	}
	for i := 1; i < 256; i++ {
		buckets[i] += buckets[i-1]
	}
	for i := 255; i > 0; i-- {
		buckets[i] = buckets[i-1]
	}
	buckets[0] = 0
	for i, c := range old {
		buckets[c]++
		I[buckets[c]] = int32(i)
	}
	I[0] = n
	for i, c := range old {
		V[i] = buckets[c]
	}
	V[n] = 0
	for i := 1; i < 256; i++ {
		if buckets[i] == buckets[i-1]+1 {
			I[buckets[i]] = -1
		}
	}
	I[0] = -1
	for h := int32(1); I[0] != -(n + 1); h += h {
		length := int32(0)
		i := int32(0)
		for i < n+1 {
			if I[i] < 0 {
				length -= I[i]
				i -= I[i]
			} else {
				if length != 0 {
					I[i-length] = -length
				}
				length = V[I[i]] + 1 - i
				bsSplit(I, V, i, length, h)
				i += length
				length = 0
			}
		}
		if length != 0 {
			I[i-length] = -length
		}
	}
	for i := int32(0); i < n+1; i++ {
		I[V[i]] = i
	}
	return I
}

func matchLen(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

func bsSearch(I []int32, old, nw []byte, st, en int) (int, int) {
	for en-st >= 2 {
		x := st + (en-st)/2
		if bytes.Compare(old[I[x]:], nw) < 0 {
			st = x
		} else {
			en = x
		}
	}
	x := matchLen(old[I[st]:], nw)
	y := matchLen(old[I[en]:], nw)
	if x > y {
		return x, int(I[st])
	}
	return y, int(I[en])
}

// bsdiff returns the control, diff and extra streams that turn old into nw.
func bsdiff(old, nw []byte) (ctrl, diff, extra []byte) {
	var ctrlB []byte
	putCtrl := func(a, b, c int) {
		ctrlB = binary.AppendVarint(ctrlB, int64(a))
		ctrlB = binary.AppendVarint(ctrlB, int64(b))
		ctrlB = binary.AppendVarint(ctrlB, int64(c))
	}
	oldsize, newsize := len(old), len(nw)
	if oldsize == 0 {
		putCtrl(0, newsize, 0)
		return ctrlB, nil, append([]byte(nil), nw...)
	}
	I := bsSuffixSort(old)
	var db, eb []byte
	var scan, pos, length int
	var lastscan, lastpos, lastoffset int
	for scan < newsize {
		oldscore := 0
		scan += length
		for scsc := scan; scan < newsize; scan++ {
			length, pos = bsSearch(I, old, nw[scan:], 0, oldsize)
			for ; scsc < scan+length; scsc++ {
				if scsc+lastoffset < oldsize && old[scsc+lastoffset] == nw[scsc] {
					oldscore++
				}
			}
			if (length == oldscore && length != 0) || length > oldscore+8 {
				break
			}
			if scan+lastoffset < oldsize && old[scan+lastoffset] == nw[scan] {
				oldscore--
			}
		}
		if length != oldscore || scan == newsize {
			s, Sf, lenf := 0, 0, 0
			for i := 0; lastscan+i < scan && lastpos+i < oldsize; {
				if old[lastpos+i] == nw[lastscan+i] {
					s++
				}
				i++
				if s*2-i > Sf*2-lenf {
					Sf = s
					lenf = i
				}
			}
			lenb := 0
			if scan < newsize {
				s, Sb := 0, 0
				for i := 1; scan >= lastscan+i && pos >= i; i++ {
					if old[pos-i] == nw[scan-i] {
						s++
					}
					if s*2-i > Sb*2-lenb {
						Sb = s
						lenb = i
					}
				}
			}
			if lastscan+lenf > scan-lenb {
				overlap := (lastscan + lenf) - (scan - lenb)
				s, Ss, lens := 0, 0, 0
				for i := 0; i < overlap; i++ {
					if nw[lastscan+lenf-overlap+i] == old[lastpos+lenf-overlap+i] {
						s++
					}
					if nw[scan-lenb+i] == old[pos-lenb+i] {
						s--
					}
					if s > Ss {
						Ss = s
						lens = i + 1
					}
				}
				lenf += lens - overlap
				lenb -= lens
			}
			for i := 0; i < lenf; i++ {
				db = append(db, nw[lastscan+i]-old[lastpos+i])
			}
			extraLen := (scan - lenb) - (lastscan + lenf)
			eb = append(eb, nw[lastscan+lenf:lastscan+lenf+extraLen]...)
			putCtrl(lenf, extraLen, (pos-lenb)-(lastpos+lenf))
			lastscan = scan - lenb
			lastpos = pos - lenb
			lastoffset = pos - scan
		}
	}
	return ctrlB, db, eb
}

// bspatch applies the streams to old and returns exactly newSize bytes.
func bspatch(old []byte, ctrl, diff, extra []byte, newSize int) ([]byte, error) {
	if newSize < 0 || newSize > 1<<31 {
		return nil, errors.New("patch: bad output size")
	}
	out := make([]byte, newSize)
	oldpos, newpos := 0, 0
	for newpos < newSize {
		var c [3]int64
		for i := range c {
			v, n := binary.Varint(ctrl)
			if n <= 0 {
				return nil, errors.New("patch: truncated control stream")
			}
			c[i] = v
			ctrl = ctrl[n:]
		}
		if c[0] < 0 || c[1] < 0 || c[0] > int64(newSize-newpos) || c[1] > int64(newSize-newpos)-c[0] {
			return nil, errors.New("patch: corrupt control stream")
		}
		n0, n1 := int(c[0]), int(c[1])
		if n0 > len(diff) || n1 > len(extra) {
			return nil, errors.New("patch: stream too short")
		}
		copy(out[newpos:], diff[:n0])
		for i := 0; i < n0; i++ {
			if p := oldpos + i; p >= 0 && p < len(old) {
				out[newpos+i] += old[p]
			}
		}
		diff = diff[n0:]
		newpos += n0
		oldpos += n0
		copy(out[newpos:], extra[:n1])
		extra = extra[n1:]
		newpos += n1
		oldpos += int(c[2])
	}
	return out, nil
}

// diffBlob packs the three streams, deflated together.
func diffBlob(old, nw []byte) ([]byte, error) {
	ctrl, diff, extra := bsdiff(old, nw)
	var raw []byte
	raw = binary.AppendUvarint(raw, uint64(len(ctrl)))
	raw = binary.AppendUvarint(raw, uint64(len(diff)))
	raw = binary.AppendUvarint(raw, uint64(len(extra)))
	raw = append(raw, ctrl...)
	raw = append(raw, diff...)
	raw = append(raw, extra...)
	return deflateBytes(raw)
}

func deflateBytes(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(raw); err != nil {
		return nil, err
	}
	if err := fw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// inflateLimited inflates blob and fails when it expands past limit.
func inflateLimited(blob []byte, limit int64) ([]byte, error) {
	fr := flate.NewReader(bytes.NewReader(blob))
	defer fr.Close()
	raw, err := io.ReadAll(io.LimitReader(fr, limit+1))
	if err != nil {
		return nil, fmt.Errorf("patch: damaged data: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("patch: data expands past its declared size")
	}
	return raw, nil
}

// applyDiffBlob reverses diffBlob.
func applyDiffBlob(old, blob []byte, newSize int) ([]byte, error) {
	raw, err := inflateLimited(blob, 4*int64(newSize)+(1<<20))
	if err != nil {
		return nil, err
	}
	var lens [3]uint64
	for i := range lens {
		v, n := binary.Uvarint(raw)
		if n <= 0 {
			return nil, errors.New("patch: damaged header")
		}
		lens[i] = v
		raw = raw[n:]
	}
	if lens[0]+lens[1]+lens[2] != uint64(len(raw)) {
		return nil, errors.New("patch: stream lengths do not add up")
	}
	return bspatch(old, raw[:lens[0]], raw[lens[0]:lens[0]+lens[1]], raw[lens[0]+lens[1]:], newSize)
}
