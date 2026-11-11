# Golden wire vectors

Each `<name>.bin` is one frame (see `../../PROTOCOL.md`). Each `<name>.json`
describes the logical value that frame carries. Both the Go (`wire`) and the TS
codec tests decode every `.bin`, compare the result with the `.json`, and
re-encode it byte for byte.

## .json format

Compact JSON, UTF-8, the same key order as the frame's JSON, with one
difference: a blob is written inline as `{"$b": "<standard base64>"}` instead
of an index. Everything else is as on the wire:

| JSON | Meaning |
| --- | --- |
| `{"$b": "<base64>"}` | binary value (the frame stores it as a blob) |
| `{"$b$": <object>}` | the user object `<object>` whose only key is `$b` or `$b$`; `<object>` is not interpreted as a blob |
| `{"$d": "<RFC 3339 UTC, ms>"}` | time |
| `{"$m": [[k, v], ...]}`, `{"$s": [v, ...]}` | Map and Set |

To compare, decode a `.bin` to this form (blob index replaced by its base64) and
compare after removing insignificant whitespace.

## Regenerating

`python3 -I gen.py` rebuilds every `.bin` from its `.json`. The generator is an
independent implementation of the encoder, so a bug in the Go or TS encoder
cannot hide in the vectors. Vectors avoid U+2028 and U+2029, which Go escapes
and Python does not; the Go tests cover them separately.
