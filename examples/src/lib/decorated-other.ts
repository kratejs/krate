function tagged(_value: undefined, _context: ClassFieldDecoratorContext) {
  return (initial: string) => initial + "-other";
}

export class OtherBox {
  @tagged value = "three";
}
