#!/usr/bin/env python3
"""Builds <name>.bin from <name>.json. See README.md for the format.

Usage: python3 gen.py   (run inside this directory)
"""
import base64, glob, json, struct

def conv(v, blobs):
    if isinstance(v, dict):
        keys = list(v)
        if keys == ["$b"] and isinstance(v["$b"], str):
            blobs.append(base64.b64decode(v["$b"]))
            return {"$b": len(blobs) - 1}
        if keys == ["$b$"]:
            # inner value is a user object; convert only its children
            return {"$b$": {k: conv(x, blobs) for k, x in v["$b$"].items()}}
        return {k: conv(x, blobs) for k, x in v.items()}
    if isinstance(v, list):
        return [conv(x, blobs) for x in v]
    return v

for path in sorted(glob.glob("*.json")):
    with open(path, encoding="utf-8") as f:
        logical = json.load(f)
    blobs = []
    wire = conv(logical, blobs)
    js = json.dumps(wire, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    out = struct.pack(">I", len(js)) + js + struct.pack(">H", len(blobs))
    for b in blobs:
        out += struct.pack(">I", len(b)) + b
    with open(path[:-5] + ".bin", "wb") as f:
        f.write(out)
