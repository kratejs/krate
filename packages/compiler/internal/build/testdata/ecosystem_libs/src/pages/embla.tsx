import useEmblaCarousel from 'embla-carousel-react';
export default function Page() {
  const [ref, api] = useEmblaCarousel({ loop: false });
  return (
    <main>
      <div ref={ref} style="overflow:hidden;width:300px" id="viewport">
        <div style="display:flex">
          <div style="flex:0 0 100%;min-width:0" id="s1">One</div>
          <div style="flex:0 0 100%;min-width:0" id="s2">Two</div>
          <div style="flex:0 0 100%;min-width:0" id="s3">Three</div>
        </div>
      </div>
      <button id="next" onClick={() => api && api.scrollNext()}>next</button>
      <p id="count">{api ? String(api.scrollSnapList().length) : 'loading'}</p>
    </main>
  );
}
