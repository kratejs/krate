function suffixed(_value: undefined, _context: ClassFieldDecoratorContext) {
  return (initial: string) => initial + "-ts";
}

export class TsBox {
  @suffixed first = "one";
  @suffixed second = "two";
}
