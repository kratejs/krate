package offscreen

import (
	"context"
	"encoding/json"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/wire"
)

func queryFrame(window string, o CaptureOptions) (Frame, error) {
	ob, _ := json.Marshal(o)
	d, err := bridge.Call(context.Background(), IDCapture, func(e *wire.Encoder) { e.String(window); e.Raw(ob) })
	if err != nil {
		return Frame{}, err
	}
	return decodeFrame(d)
}

// Frames calls fn for each frame of the window until cancel is called or the
// stream ends. fn runs before the next capture starts, so a slow fn lowers the
// frame rate rather than growing a queue.
func Frames(window string, o CaptureOptions, fn func(Frame)) (cancel func(), err error) {
	ob, _ := json.Marshal(o)
	return bridge.SubscribeArgs(context.Background(), IDFrames, func(e *wire.Encoder) { e.String(window); e.Raw(ob) }, func(d *wire.Decoder) {
		if f, err := decodeFrame(d); err == nil {
			fn(f)
		}
	})
}

func decodeFrame(d *wire.Decoder) (Frame, error) {
	var f Frame
	if err := d.BeginObject(); err != nil {
		return f, err
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return f, err
		}
		if k == "data" {
			if f.Data, err = d.Bytes(); err != nil {
				return f, err
			}
			continue
		}
		raw, err := d.RawValue()
		if err != nil {
			return f, err
		}
		switch k {
		case "seq":
			_ = json.Unmarshal(raw, &f.Seq)
		case "timeMs":
			_ = json.Unmarshal(raw, &f.TimeMs)
		case "width":
			_ = json.Unmarshal(raw, &f.Width)
		case "height":
			_ = json.Unmarshal(raw, &f.Height)
		case "format":
			_ = json.Unmarshal(raw, &f.Format)
		case "dropped":
			_ = json.Unmarshal(raw, &f.Dropped)
		}
	}
	return f, nil
}
