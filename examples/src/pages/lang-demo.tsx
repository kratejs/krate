export const lang = "fr";

export default function LangDemo() {
  return (
    <main>
      <h1>Per-page language</h1>
      <p>
        This page sets <code>export const lang = "fr"</code>, so the document
        ships <code>&lt;html lang="fr"&gt;</code>.
      </p>
    </main>
  );
}
