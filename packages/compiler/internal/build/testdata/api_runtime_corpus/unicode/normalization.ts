export async function GET() {
  const s = "e\u0301";
  return Response.json({ nfc: s.normalize("NFC").length, nfd: "\u00e9".normalize("NFD").length, upper: "stra\u00dfe".toUpperCase(), lower: "\u0130".toLowerCase().length, });
}
