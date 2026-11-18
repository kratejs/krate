// A helper that reads the global Buffer, which the Krate embedded runtime lacks.
export function encode(text: string): string {
  return Buffer.from(text, 'utf8').toString('base64');
}
