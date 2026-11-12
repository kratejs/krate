package offscreen

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func snap(w, h, stride int, px func(x, y int) [4]byte) caps.Snapshot {
	s := caps.Snapshot{Width: w, Height: h, Stride: stride, Pix: make([]byte, stride*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := px(x, y)
			copy(s.Pix[y*stride+x*4:], p[:])
		}
	}
	return s
}

func TestEncodePNGKeepsColorsAndUnpremultiplies(t *testing.T) {
	// BGRA premultiplied: opaque red, half-transparent premultiplied blue, clear.
	s := snap(3, 1, 16, func(x, y int) [4]byte {
		return [][4]byte{{0, 0, 255, 255}, {128, 0, 0, 128}, {0, 0, 0, 0}}[x]
	})
	b, err := EncodePNG(s)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 3, 1) {
		t.Fatal(img.Bounds())
	}
	n := img.(*image.NRGBA)
	if c := n.NRGBAAt(0, 0); c.R != 255 || c.G != 0 || c.B != 0 || c.A != 255 {
		t.Fatalf("red: %+v", c)
	}
	if c := n.NRGBAAt(1, 0); c.B < 254 || c.A != 128 || c.R != 0 {
		t.Fatalf("unpremultiplied blue: %+v", c)
	}
	if c := n.NRGBAAt(2, 0); c.A != 0 {
		t.Fatalf("clear: %+v", c)
	}
}

func TestRawBGRARemovesRowPadding(t *testing.T) {
	s := snap(2, 2, 12, func(x, y int) [4]byte { return [4]byte{byte(x), byte(y), 9, 255} })
	raw := RawBGRA(s)
	if len(raw) != 16 {
		t.Fatalf("len %d", len(raw))
	}
	want := []byte{0, 0, 9, 255, 1, 0, 9, 255, 0, 1, 9, 255, 1, 1, 9, 255}
	if !bytes.Equal(raw, want) {
		t.Fatalf("%v", raw)
	}
	tight := snap(2, 1, 8, func(x, y int) [4]byte { return [4]byte{1, 2, 3, 4} })
	if got := RawBGRA(tight); len(got) != 8 || &got[0] != &tight.Pix[0] {
		t.Fatal("a tight snapshot should not be copied")
	}
}

func TestPacing(t *testing.T) {
	if ClampFPS(0) != DefaultFPS || ClampFPS(500) != MaxFPS || ClampFPS(5) != 5 {
		t.Fatal("clamp")
	}
	if Interval(20) != 50*time.Millisecond {
		t.Fatal(Interval(20))
	}
	iv := 100 * time.Millisecond
	for busy, want := range map[time.Duration]int{0: 0, 50 * time.Millisecond: 0, 100 * time.Millisecond: 0, 250 * time.Millisecond: 1, 350 * time.Millisecond: 2} {
		if got := Missed(busy, iv); got != want {
			t.Errorf("Missed(%v) = %d, want %d", busy, got, want)
		}
	}
}

func TestStreamPacesAndCountsDroppedFrames(t *testing.T) {
	capture := func() (caps.Snapshot, error) {
		return snap(2, 2, 8, func(x, y int) [4]byte { return [4]byte{1, 2, 3, 255} }), nil
	}
	var got []Frame
	start := time.Now()
	err := stream(context.Background(), CaptureOptions{FPS: 50, Format: FormatBGRA, MaxFrames: 4}, capture, func(f Frame) error {
		got = append(got, f)
		if f.Seq == 2 {
			time.Sleep(70 * time.Millisecond) // 3.5 intervals at 50 fps
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d frames", len(got))
	}
	for i, f := range got {
		if f.Seq != i+1 || f.Format != FormatBGRA || len(f.Data) != 16 {
			t.Fatalf("frame %d: %+v", i, f)
		}
	}
	if got[2].Dropped < 2 || got[1].Dropped != 0 {
		t.Fatalf("a slow consumer should show as dropped frames on the next one: %+v", []int{got[0].Dropped, got[1].Dropped, got[2].Dropped, got[3].Dropped})
	}
	if time.Since(start) < 3*20*time.Millisecond {
		t.Fatal("the stream did not wait between frames")
	}
}

func TestStreamStopsWithTheContextAndReportsCaptureErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := stream(ctx, CaptureOptions{FPS: 60, Format: FormatPNG}, func() (caps.Snapshot, error) {
		return snap(1, 1, 4, func(x, y int) [4]byte { return [4]byte{0, 0, 0, 255} }), nil
	}, func(Frame) error {
		if n++; n == 3 {
			cancel()
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("err=%v n=%d", err, n)
	}
	boom := errors.New("boom")
	if err := stream(context.Background(), CaptureOptions{}, func() (caps.Snapshot, error) { return caps.Snapshot{}, boom }, nil); err != boom {
		t.Fatal(err)
	}
}
