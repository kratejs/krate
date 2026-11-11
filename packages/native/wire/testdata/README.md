# Golden wire vectors

The single source of truth shared by the Go (`wire`) and TypeScript
(`@krate/app`) codec tests. Each `<name>.bin` is one frame (see
`../../PROTOCOL.md`). Each `<name>.json` describes the logical value that frame
carries. Both test suites decode every `.bin`, compare the result with the
`.json`, and re-encode it byte for byte unless a flags file says otherwise.

Go: `wire/golden_test.go`. TS: `packages/app/test/shared-vectors.test.ts`
(reads this directory through node fs).

## Only canonical forms

A vector must be a frame that BOTH encoders would emit byte for byte for the
value it describes: no whitespace, insertion key order, shortest number text,
the escapes listed below. Forms that only one side emits, or that only one side
accepts, belong in that side's own tests (`wire/compat_test.go`,
`packages/app/test/go-forms.test.ts`), not here. Known one-sided forms:

- `-0`: Go emits `-0`, `JSON.stringify` emits `0` (see `numbers_neg_zero`).
- U+2028 and U+2029: Go escapes them as ` `/` `, JS leaves them raw.
- Lone surrogates: JS emits `\udXXX`, Go has no such string (it writes `�`).
- Map and Set: Go decodes them as lists and encodes them with `BeginMap`/`BeginSet`.
- Integer-like keys: JS orders them first and ascending; vectors that have them
  list them in that order already (`int_keys`).
- Integers beyond 2^53: exact in Go, rounded by `JSON.parse`; not shared.

## .json format

Compact JSON, UTF-8. It is the frame's JSON with these differences: a blob is
written inline as `{"$b": "<standard base64>"}` instead of an index, and the
text of every number is exactly the text on the wire (so `1e+21`, `5e-324`).

| JSON | Meaning |
| --- | --- |
| `{"$b": "<base64>"}` | binary value (the frame stores it as a blob) |
| `{"$d": "<RFC 3339 UTC, ms>"}` | time |
| `{"$m": [[k, v], ...]}` | Map (entries in order; keys of any kind) |
| `{"$s": [v, ...]}` | Set |
| `{"<tag>$": <object>}`, tag in `$b $d $m $s` | the user object `<object>`; it is a plain object whose keys are taken literally and whose values decode as usual |
| any other object | a plain object (an object with several keys is always plain, even if one key is `$b`) |

A tag is only a tag when it is the sole key. The description therefore never
contains a sole unescaped `$b`/`$d`/`$m`/`$s` key except as the real tagged
value.

To compare, decode a `.bin` to this form (blob index replaced by its base64)
and compare after removing insignificant whitespace.

## .flags.json (optional)

`<name>.flags.json` next to a vector marks which re-encode checks to skip:

```json
{"goEncode": false, "tsEncode": false, "reason": "why"}
```

`goEncode: false` skips the Go re-encode (the Go side still decodes and
compares); `tsEncode: false` skips the TS re-encode. `reason` is required when
either is false. Absent flags mean both re-encodes must match.

## Regenerating

`python3 -I gen.py` rebuilds every `.bin` from its `.json`. The generator is an
independent implementation of the encoder, so a bug in the Go or TS encoder
cannot hide in the vectors. It keeps number text as written. Vectors avoid
U+2028 and U+2029, which Go escapes and Python does not.
