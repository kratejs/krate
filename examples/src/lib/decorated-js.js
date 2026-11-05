function upper(method) {
  return function (...args) {
    return method.apply(this, args).toUpperCase();
  };
}

export class JsGreeter {
  @upper
  greet() {
    return "from js";
  }
}
