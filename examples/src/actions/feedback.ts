export async function saveFeedback(input: FormData | { text: string }) {
  const text =
    input instanceof FormData ? String(input.get("text") || "") : String(input.text || "");
  return {
    text: text.trim(),
    length: text.trim().length,
    savedAt: new Date().toISOString(),
  };
}
