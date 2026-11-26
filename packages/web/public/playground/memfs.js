// In-memory Node-style fs for the Go js/wasm runtime.
//
// Go's os package calls globalThis.fs (callback API, see syscall/fs_js.go)
// plus a few globalThis.process and globalThis.path functions. This module
// implements the subset Go uses over a tree held in memory, so the compiler
// can run in a browser worker, and in Node without touching the real disk.

const S_IFDIR = 0o040000;
const S_IFREG = 0o100000;
const S_IFLNK = 0o120000;

const O_WRONLY = 1;
const O_RDWR = 2;
const O_CREAT = 0o100;
const O_EXCL = 0o200;
const O_TRUNC = 0o1000;
const O_APPEND = 0o2000;
const O_DIRECTORY = 0o200000;

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8");

function fsError(code, syscall, path) {
  const err = new Error(`${code}: ${syscall} '${path}'`);
  err.code = code;
  err.syscall = syscall;
  err.path = path;
  return err;
}

/** Normalises an absolute path: collapses ., .. and repeated slashes. */
function normalize(p) {
  const out = [];
  for (const seg of p.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") out.pop();
    else out.push(seg);
  }
  return "/" + out.join("/");
}

export function createMemFS(options = {}) {
  let cwd = "/";
  let nextIno = 1;
  const now = () => Date.now();

  function newNode(type, mode) {
    const t = now();
    return {
      type, // "file" | "dir" | "link"
      mode,
      ino: nextIno++,
      data: type === "file" ? new Uint8Array(0) : null,
      size: 0,
      children: type === "dir" ? new Map() : null,
      target: null,
      nlink: type === "dir" ? 2 : 1,
      atime: t,
      mtime: t,
      ctime: t,
    };
  }

  const root = newNode("dir", 0o755);

  function abs(p) {
    return normalize(p.startsWith("/") ? p : cwd + "/" + p);
  }

  /** Resolves a path to its node, following symlinks (except a final one when noFollow). */
  function lookup(p, noFollow = false, depth = 0) {
    if (depth > 32) throw fsError("ELOOP", "stat", p);
    const full = abs(p);
    let node = root;
    let cur = "";
    const segs = full.split("/").filter(Boolean);
    for (let i = 0; i < segs.length; i++) {
      if (node.type !== "dir") throw fsError("ENOTDIR", "stat", p);
      const child = node.children.get(segs[i]);
      if (!child) throw fsError("ENOENT", "stat", p);
      cur += "/" + segs[i];
      if (child.type === "link" && (!noFollow || i < segs.length - 1)) {
        const rest = segs.slice(i + 1).join("/");
        const target = child.target.startsWith("/") ? child.target : cur.slice(0, cur.lastIndexOf("/")) + "/" + child.target;
        return lookup(rest ? target + "/" + rest : target, noFollow, depth + 1);
      }
      node = child;
    }
    return node;
  }

  /** Returns [parent dir node, base name] of a path. */
  function parentOf(p, syscall) {
    const full = abs(p);
    if (full === "/") throw fsError("EEXIST", syscall, p);
    const i = full.lastIndexOf("/");
    const dir = lookup(full.slice(0, i) || "/");
    if (dir.type !== "dir") throw fsError("ENOTDIR", syscall, p);
    return [dir, full.slice(i + 1)];
  }

  function statOf(node) {
    const size = node.type === "file" ? node.size : node.type === "link" ? node.target.length : 4096;
    const st = {
      dev: 1,
      ino: node.ino,
      mode: (node.type === "dir" ? S_IFDIR : node.type === "link" ? S_IFLNK : S_IFREG) | node.mode,
      nlink: node.nlink,
      uid: 0,
      gid: 0,
      rdev: 0,
      size,
      blksize: 4096,
      blocks: Math.ceil(size / 512),
      atimeMs: node.atime,
      mtimeMs: node.mtime,
      ctimeMs: node.ctime,
      isDirectory: () => node.type === "dir",
      isFile: () => node.type === "file",
      isSymbolicLink: () => node.type === "link",
    };
    return st;
  }

  // File descriptors. 0-2 are the console.
  const fds = new Map();
  let nextFd = 3;

  let outBuf = { 1: "", 2: "" };
  const onOutput = options.onOutput || ((fd, text) => (fd === 2 ? console.error(text) : console.log(text)));
  function writeConsole(fd, buf) {
    outBuf[fd] += decoder.decode(buf, { stream: true });
    const nl = outBuf[fd].lastIndexOf("\n");
    if (nl !== -1) {
      onOutput(fd, outBuf[fd].slice(0, nl));
      outBuf[fd] = outBuf[fd].slice(nl + 1);
    }
    return buf.length;
  }

  function getFd(fd, syscall) {
    const f = fds.get(fd);
    if (!f) throw fsError("EBADF", syscall, String(fd));
    return f;
  }

  function ensureCapacity(node, n) {
    if (node.data.length >= n) return;
    const next = new Uint8Array(Math.max(n, Math.min(node.data.length * 2, node.data.length + (1 << 20)), 64));
    next.set(node.data.subarray(0, node.size));
    node.data = next;
  }

  function writeAt(node, src, offset, length, position) {
    const end = position + length;
    ensureCapacity(node, end);
    if (position > node.size) node.data.fill(0, node.size, position);
    node.data.set(src.subarray(offset, offset + length), position);
    if (end > node.size) node.size = end;
    node.mtime = node.ctime = now();
    return length;
  }

  function truncateNode(node, length) {
    if (length < node.size) node.size = length;
    else if (length > node.size) {
      ensureCapacity(node, length);
      node.data.fill(0, node.size, length);
      node.size = length;
    }
    node.mtime = node.ctime = now();
  }

  function removeNode(p, wantDir, syscall) {
    const [dir, name] = parentOf(p, syscall);
    const node = dir.children.get(name);
    if (!node) throw fsError("ENOENT", syscall, p);
    if (wantDir) {
      if (node.type !== "dir") throw fsError("ENOTDIR", syscall, p);
      if (node.children.size > 0) throw fsError("ENOTEMPTY", syscall, p);
      dir.nlink--;
    } else if (node.type === "dir") {
      throw fsError("EISDIR", syscall, p);
    }
    dir.children.delete(name);
    dir.mtime = dir.ctime = now();
  }

  function mkdirNode(p, mode, syscall = "mkdir") {
    const [dir, name] = parentOf(p, syscall);
    if (dir.children.has(name)) throw fsError("EEXIST", syscall, p);
    const node = newNode("dir", mode & 0o7777);
    dir.children.set(name, node);
    dir.nlink++;
    dir.mtime = dir.ctime = now();
    return node;
  }

  /** Runs body, delivering its result or error through a Node-style callback. */
  function run(callback, body) {
    let result;
    try {
      result = body();
    } catch (e) {
      if (e && e.code) {
        callback(e);
        return;
      }
      throw e;
    }
    callback(null, result);
  }

  const fs = {
    constants: { O_RDONLY: 0, O_WRONLY, O_RDWR, O_CREAT, O_EXCL, O_TRUNC, O_APPEND, O_DIRECTORY },

    writeSync(fd, buf) {
      if (fd === 1 || fd === 2) return writeConsole(fd, buf);
      const f = getFd(fd, "write");
      return writeAt(f.node, buf, 0, buf.length, f.flags & O_APPEND ? f.node.size : f.pos);
    },

    open(path, flags, mode, callback) {
      run(callback, () => {
        let node;
        try {
          node = lookup(path);
          if (flags & O_CREAT && flags & O_EXCL) throw fsError("EEXIST", "open", path);
        } catch (e) {
          if (e.code !== "ENOENT" || !(flags & O_CREAT)) throw e;
          const [dir, name] = parentOf(path, "open");
          node = newNode("file", mode & 0o7777);
          dir.children.set(name, node);
          dir.mtime = dir.ctime = now();
        }
        if (node.type === "dir" && flags & (O_WRONLY | O_RDWR)) throw fsError("EISDIR", "open", path);
        if (flags & O_DIRECTORY && node.type !== "dir") throw fsError("ENOTDIR", "open", path);
        if (flags & O_TRUNC && node.type === "file") truncateNode(node, 0);
        const fd = nextFd++;
        fds.set(fd, { node, flags, pos: 0, path: abs(path) });
        return fd;
      });
    },

    close(fd, callback) {
      run(callback, () => {
        getFd(fd, "close");
        fds.delete(fd);
      });
    },

    read(fd, buffer, offset, length, position, callback) {
      run(callback, () => {
        const f = getFd(fd, "read");
        if (f.node.type === "dir") throw fsError("EISDIR", "read", f.path);
        const from = position === null || position === undefined ? f.pos : position;
        const n = Math.max(0, Math.min(length, f.node.size - from));
        buffer.set(f.node.data.subarray(from, from + n), offset);
        if (position === null || position === undefined) f.pos += n;
        return n;
      });
    },

    write(fd, buffer, offset, length, position, callback) {
      run(callback, () => {
        if (fd === 1 || fd === 2) return writeConsole(fd, buffer.subarray(offset, offset + length));
        const f = getFd(fd, "write");
        if (!(f.flags & (O_WRONLY | O_RDWR))) throw fsError("EBADF", "write", f.path);
        const at = position === null || position === undefined ? (f.flags & O_APPEND ? f.node.size : f.pos) : position;
        const n = writeAt(f.node, buffer, offset, length, at);
        if (position === null || position === undefined) f.pos = at + n;
        return n;
      });
    },

    fstat(fd, callback) {
      run(callback, () => statOf(getFd(fd, "fstat").node));
    },
    stat(path, callback) {
      run(callback, () => statOf(lookup(path)));
    },
    lstat(path, callback) {
      run(callback, () => statOf(lookup(path, true)));
    },

    readdir(path, callback) {
      run(callback, () => {
        const node = lookup(path);
        if (node.type !== "dir") throw fsError("ENOTDIR", "readdir", path);
        return [...node.children.keys()].sort();
      });
    },

    mkdir(path, mode, callback) {
      run(callback, () => {
        mkdirNode(path, mode);
      });
    },
    rmdir(path, callback) {
      run(callback, () => removeNode(path, true, "rmdir"));
    },
    unlink(path, callback) {
      run(callback, () => removeNode(path, false, "unlink"));
    },

    rename(from, to, callback) {
      run(callback, () => {
        const [sdir, sname] = parentOf(from, "rename");
        const node = sdir.children.get(sname);
        if (!node) throw fsError("ENOENT", "rename", from);
        const [ddir, dname] = parentOf(to, "rename");
        const existing = ddir.children.get(dname);
        if (existing) {
          if (existing.type === "dir" && existing.children.size > 0) throw fsError("ENOTEMPTY", "rename", to);
          if (existing.type === "dir" && node.type !== "dir") throw fsError("EISDIR", "rename", to);
          if (existing.type !== "dir" && node.type === "dir") throw fsError("ENOTDIR", "rename", to);
          if (existing.type === "dir") ddir.nlink--;
        }
        sdir.children.delete(sname);
        ddir.children.set(dname, node);
        if (node.type === "dir" && sdir !== ddir) {
          sdir.nlink--;
          ddir.nlink++;
        }
        sdir.mtime = ddir.mtime = node.ctime = now();
      });
    },

    ftruncate(fd, length, callback) {
      run(callback, () => truncateNode(getFd(fd, "ftruncate").node, length));
    },
    truncate(path, length, callback) {
      run(callback, () => truncateNode(lookup(path), length));
    },

    readlink(path, callback) {
      run(callback, () => {
        const node = lookup(path, true);
        if (node.type !== "link") throw fsError("EINVAL", "readlink", path);
        return node.target;
      });
    },
    symlink(target, path, callback) {
      run(callback, () => {
        const [dir, name] = parentOf(path, "symlink");
        if (dir.children.has(name)) throw fsError("EEXIST", "symlink", path);
        const node = newNode("link", 0o777);
        node.target = target;
        dir.children.set(name, node);
      });
    },
    link(existing, path, callback) {
      run(callback, () => {
        const node = lookup(existing);
        if (node.type === "dir") throw fsError("EPERM", "link", existing);
        const [dir, name] = parentOf(path, "link");
        if (dir.children.has(name)) throw fsError("EEXIST", "link", path);
        dir.children.set(name, node);
        node.nlink++;
      });
    },

    utimes(path, atime, mtime, callback) {
      run(callback, () => {
        const node = lookup(path);
        node.atime = atime * 1000;
        node.mtime = mtime * 1000;
      });
    },
    chmod(path, mode, callback) {
      run(callback, () => {
        lookup(path).mode = mode & 0o7777;
      });
    },
    fchmod(fd, mode, callback) {
      run(callback, () => {
        getFd(fd, "fchmod").node.mode = mode & 0o7777;
      });
    },
    chown(path, uid, gid, callback) {
      run(callback, () => lookup(path));
    },
    fchown(fd, uid, gid, callback) {
      run(callback, () => getFd(fd, "fchown"));
    },
    lchown(path, uid, gid, callback) {
      run(callback, () => lookup(path, true));
    },
    fsync(fd, callback) {
      callback(null);
    },
  };

  const process = {
    getuid: () => 0,
    getgid: () => 0,
    geteuid: () => 0,
    getegid: () => 0,
    getgroups: () => [0],
    pid: 1,
    ppid: 0,
    umask: () => 0o022,
    cwd: () => cwd,
    chdir(dir) {
      const full = abs(dir);
      if (lookup(full).type !== "dir") throw fsError("ENOTDIR", "chdir", dir);
      cwd = full;
    },
  };

  const path = { resolve: (...segs) => segs.reduce((acc, s) => (s.startsWith("/") ? normalize(s) : normalize(acc + "/" + s)), cwd) };

  // Direct helpers for the host side (no callbacks, no Go round trip).
  const api = {
    fs,
    process,
    path,
    exists(p) {
      try {
        lookup(p);
        return true;
      } catch {
        return false;
      }
    },
    mkdirp(p) {
      let cur = "";
      for (const seg of abs(p).split("/").filter(Boolean)) {
        cur += "/" + seg;
        const parent = lookup(cur.slice(0, cur.lastIndexOf("/")) || "/");
        if (!parent.children.has(seg)) mkdirNode(cur, 0o755);
      }
    },
    writeFile(p, data) {
      const bytes = typeof data === "string" ? encoder.encode(data) : data;
      const full = abs(p);
      api.mkdirp(full.slice(0, full.lastIndexOf("/")) || "/");
      const [dir, name] = parentOf(full, "open");
      let node = dir.children.get(name);
      if (!node) {
        node = newNode("file", 0o644);
        dir.children.set(name, node);
      }
      node.data = bytes.slice();
      node.size = bytes.length;
      node.mtime = node.ctime = now();
    },
    readFile(p) {
      const node = lookup(p);
      if (node.type !== "file") throw fsError("EISDIR", "read", p);
      return node.data.slice(0, node.size);
    },
    readText(p) {
      return decoder.decode(api.readFile(p));
    },
    list(p) {
      const node = lookup(p);
      return node.type === "dir" ? [...node.children.keys()].sort() : [];
    },
    /** Every file path under dir, relative to dir. */
    walk(dir) {
      const out = [];
      const visit = (node, prefix) => {
        for (const [name, child] of node.children) {
          if (child.type === "dir") visit(child, prefix + name + "/");
          else out.push(prefix + name);
        }
      };
      visit(lookup(dir), "");
      return out.sort();
    },
    remove(p) {
      let node;
      try {
        node = lookup(p, true);
      } catch {
        return;
      }
      if (node.type === "dir") for (const name of [...node.children.keys()]) api.remove(abs(p) + "/" + name);
      removeNode(p, node.type === "dir", "remove");
    },
    /** Installs fs, process and path as the globals Go reads at start up. */
    install(target = globalThis) {
      target.fs = fs;
      target.process = process;
      target.path = path;
    },
  };
  return api;
}
