import { Playground } from "../components/playground";

export default function PlaygroundPage() {
  return (
    <>
      <Head>
        <title>Playground</title>
        <meta
          name="description"
          content="Write TSX and build it with the real Krate compiler, running in your browser as WebAssembly. Edit files, see the generated HTML, JS and CSS, and share a link."
        />
      </Head>
      <Playground />
    </>
  );
}
