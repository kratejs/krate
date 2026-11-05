# Third-party test samples

These files are unmodified copies of real-world source code. They are test input
for the highlighter parity test (`TestParityCorpus`), which compares the runtime
tokenizer with chroma on code that people actually wrote. They are not compiled,
shipped or linked into Krate.

## python/

Copies of modules from the CPython 3.13 standard library (`configparser`,
`dataclasses`, `textwrap`, `shlex`, `string`, `fnmatch`, and `tomllib/_parser.py`
as `tomllib_parser.py`). Copyright (c) 2001-2024 Python Software Foundation. All
rights reserved. Used under the Python Software Foundation License Version 2
(https://docs.python.org/3/license.html).

## rust/

Source files from these crates, each available under "MIT OR Apache-2.0"; they are
used here under the MIT license:

| File prefix | Crate | Copyright |
|-------------|-------|-----------|
| `anyhow_` | anyhow 1.0.104 | David Tolnay |
| `once_cell_` | once_cell 1.21.4 | Aleksey Kladov |
| `smallvec_` | smallvec 1.16.4 | The Servo Project Developers |
| `semver_` | semver 1.0.28 | David Tolnay |
| `itoa_` | itoa 1.0.18 | David Tolnay |
| `bitflags_` | bitflags 2.13.2 | The Rust Project Developers |

## toml/

`Cargo.toml` manifests of published crates (axum, backtrace, bitflags, blake3,
anyhow, smallvec, once_cell, semver, memchr, regex-syntax, aho-corasick, arrayvec,
base64, bytemuck, comrak). These are package metadata, copied unmodified; each crate
is available under a permissive open source license (MIT, Apache-2.0, BSD-2-Clause,
Unlicense, Zlib or CC0-1.0).

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in the
Software without restriction, including without limitation the rights to use,
copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the
Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN
AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
