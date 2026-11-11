#!/usr/bin/env python3
"""Builds <name>.bin from <name>.json. See README.md for the format.

Usage: python3 -I gen.py   (run inside this directory)

Numbers keep their source text, so the .json files spell numbers exactly as
both codecs emit them (for example 1e+21 and 5e-324).
"""
import base64, glob, json, struct

ESC = ("$b$", "$d$", "$m$", "$s$")

class Num(str):
    """A JSON number kept as its source text."""

def conv(v, blobs):
    """Description -> wire tree (blob inlined as base64 becomes an index)."""
    if isinstance(v, dict):
        keys = list(v)
        if keys == ["$b"] and isinstance(v["$b"], str):
            blobs.append(base64.b64decode(v["$b"]))
            return {"$b": len(blobs) - 1}
        if len(keys) == 1 and keys[0] in ESC:
            # the inner value is a user object: convert only its children
            return {keys[0]: {k: conv(x, blobs) for k, x in v[keys[0]].items()}}
        if keys == ["$m"]:
            return {"$m": [[conv(x, blobs) for x in pair] for pair in v["$m"]]}
        if keys == ["$s"]:
            return {"$s": [conv(x, blobs) for x in v["$s"]]}
        return {k: conv(x, blobs) for k, x in v.items()}
    if isinstance(v, list):
        return [conv(x, blobs) for x in v]
    return v

def dump(v):
    if isinstance(v, Num):
        return str(v)
    if isinstance(v, dict):
        return "{" + ",".join(json.dumps(k, ensure_ascii=False) + ":" + dump(x) for k, x in v.items()) + "}"
    if isinstance(v, list):
        return "[" + ",".join(dump(x) for x in v) + "]"
    return json.dumps(v, ensure_ascii=False, separators=(",", ":"))

for path in sorted(glob.glob("*.json")):
    if path.endswith(".flags.json"):
        continue
    with open(path, encoding="utf-8") as f:
        logical = json.load(f, parse_float=Num, parse_int=Num)
    blobs = []
    wire = conv(logical, blobs)
    js = dump(wire).encode("utf-8")
    out = struct.pack(">I", len(js)) + js + struct.pack(">H", len(blobs))
    for b in blobs:
        out += struct.pack(">I", len(b)) + b
    with open(path[:-5] + ".bin", "wb") as f:
        f.write(out)
