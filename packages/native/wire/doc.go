// Package wire implements the frame codec of the host call protocol (see
// PROTOCOL.md). A frame is a u32 big-endian JSON length, the JSON, a u16
// big-endian blob count and the blobs, each a u32 big-endian length followed by
// its bytes.
//
// Encoder and Decoder are deliberately low level: generated glue calls them
// directly, so no reflection runs on the call path.
package wire
