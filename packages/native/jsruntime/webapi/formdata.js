// Blob, File and FormData feature, with multipart parsing in the Go host
// (__go_parse_multipart).
(function (G, K) {
  'use strict';
  var def = K.def, unsupported = K.unsupported, randomHex = K.randomHex;
  var toBytes = K.toBytes, concatBytes = K.concatBytes, utf8Encode = K.utf8Encode, utf8Decode = K.utf8Decode;
  var toHost = K.toHost, fromHost = K.fromHost, host = K.host;
  var goParseMultipart = G.__go_parse_multipart;

  class Blob {
    constructor(parts, options) {
      var list = [];
      if (parts !== undefined && parts !== null) {
        for (var p of parts) {
          if (p instanceof Blob) list.push(p._b);
          else { var b = toBytes(p); list.push(b ? b : utf8Encode(String(p))); }
        }
      }
      Object.defineProperty(this, '_b', { value: concatBytes(list), writable: true, enumerable: false });
      this.type = options && options.type ? String(options.type).toLowerCase() : '';
    }
    get size() { return this._b.length; }
    slice(start, end, type) {
      var b = new Blob([], { type: type || '' });
      b._b = this._b.slice(start, end);
      return b;
    }
    arrayBuffer() { var c = this._b.slice(); return Promise.resolve(c.buffer); }
    bytes() { return Promise.resolve(this._b.slice()); }
    text() { return Promise.resolve(utf8Decode(this._b)); }
    stream() { return K.need('streams').streamFromBytes(this._b); }
  }
  class File extends Blob {
    constructor(parts, name, options) {
      super(parts, options);
      this.name = String(name);
      this.lastModified = options && options.lastModified !== undefined ? Number(options.lastModified) : Date.now();
    }
  }
  def('Blob', Blob);
  def('File', File);

  function toFormValue(name, value, filename) {
    if (value instanceof Blob) {
      if (!(value instanceof File) || filename !== undefined) value = new File([value], filename !== undefined ? String(filename) : 'blob', { type: value.type });
      return value;
    }
    return String(value);
  }
  class FormData {
    constructor(form) {
      if (form !== undefined) unsupported('FormData from an HTML form');
      Object.defineProperty(this, '_list', { value: [], writable: true, enumerable: false });
    }
    append(name, value, filename) { this._list.push([String(name), toFormValue(name, value, filename)]); }
    delete(name) { name = String(name); this._list = this._list.filter(function (p) { return p[0] !== name; }); }
    get(name) { name = String(name); for (var i = 0; i < this._list.length; i++) if (this._list[i][0] === name) return this._list[i][1]; return null; }
    getAll(name) { name = String(name); return this._list.filter(function (p) { return p[0] === name; }).map(function (p) { return p[1]; }); }
    has(name) { name = String(name); return this._list.some(function (p) { return p[0] === name; }); }
    set(name, value, filename) {
      name = String(name);
      var v = toFormValue(name, value, filename), found = false, out = [];
      for (var i = 0; i < this._list.length; i++) {
        var p = this._list[i];
        if (p[0] === name) { if (!found) { found = true; out.push([name, v]); } } else out.push(p);
      }
      if (!found) out.push([name, v]);
      this._list = out;
    }
    forEach(cb, thisArg) { for (var i = 0; i < this._list.length; i++) cb.call(thisArg, this._list[i][1], this._list[i][0], this); }
    *keys() { for (var i = 0; i < this._list.length; i++) yield this._list[i][0]; }
    *values() { for (var i = 0; i < this._list.length; i++) yield this._list[i][1]; }
    *entries() { for (var i = 0; i < this._list.length; i++) yield [this._list[i][0], this._list[i][1]]; }
    [Symbol.iterator]() { return this.entries(); }
  }
  def('FormData', FormData);

  function serializeMultipart(fd) {
    var boundary = '----KrateFormBoundary' + randomHex(12), parts = [];
    function esc(s) { return String(s).replace(/\r?\n|\r/g, '%0D%0A').replace(/"/g, '%22'); }
    for (var i = 0; i < fd._list.length; i++) {
      var name = fd._list[i][0], v = fd._list[i][1];
      if (typeof v === 'string') {
        parts.push(utf8Encode('--' + boundary + '\r\nContent-Disposition: form-data; name="' + esc(name) + '"\r\n\r\n' + v.replace(/\r?\n|\r/g, '\r\n') + '\r\n'));
      } else {
        parts.push(utf8Encode('--' + boundary + '\r\nContent-Disposition: form-data; name="' + esc(name) + '"; filename="' + esc(v.name) + '"\r\nContent-Type: ' + (v.type || 'application/octet-stream') + '\r\n\r\n'));
        parts.push(v._b);
        parts.push(utf8Encode('\r\n'));
      }
    }
    parts.push(utf8Encode('--' + boundary + '--\r\n'));
    return { bytes: concatBytes(parts), type: 'multipart/form-data; boundary=' + boundary };
  }
  // multipartFromParsed builds a FormData from the host's description of a
  // multipart body; the part bytes follow from __go_take.
  function multipartFromParsed(parsed) {
    var res = JSON.parse(parsed), fd = new FormData();
    if (res.error) throw new TypeError('Could not parse content as FormData: ' + res.error);
    var data = fromHost(host(G.__go_take));
    for (var j = 0; j < res.parts.length; j++) {
      var p = res.parts[j], b = data.subarray(p.off, p.off + p.len);
      if (p.filename !== undefined) fd._list.push([p.name, new File([b], p.filename, { type: p.type || '' })]);
      else fd._list.push([p.name, utf8Decode(b)]);
    }
    return fd;
  }
  function parseFormBody(contentType, bytes) {
    var ct = String(contentType || '').toLowerCase();
    var fd = new FormData();
    if (ct.indexOf('application/x-www-form-urlencoded') === 0) {
      var list = K.need('url').parseForm(utf8Decode(bytes));
      for (var i = 0; i < list.length; i++) fd.append(list[i][0], list[i][1]);
      return fd;
    }
    if (ct.indexOf('multipart/form-data') === 0) return multipartFromParsed(host(goParseMultipart, String(contentType), toHost(bytes)));
    throw new TypeError('Could not parse content as FormData: unsupported content-type');
  }

  K.mods.formdata = { Blob: Blob, File: File, FormData: FormData, serializeMultipart: serializeMultipart, multipartFromParsed: multipartFromParsed, parseFormBody: parseFormBody };
})(globalThis, globalThis[Symbol.for('krate.web')]);
