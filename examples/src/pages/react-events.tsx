import { useState, useRef, useEffect, useLayoutEffect, useImperativeHandle, forwardRef } from 'react';

// React 19 ref-as-prop: no forwardRef needed.
function FocusInput({ ref, ...rest }: any) {
  return <input ref={ref} {...rest} />;
}

const Counter = forwardRef((props: any, ref: any) => {
  const [n, setN] = useState(0);
  useImperativeHandle(ref, () => ({ bump: () => setN((v: number) => v + 1) }), []);
  return <span data-testid="handle-count">{n}</span>;
});

export default function Page() {
  const [log, setLog] = useState('');
  const [upper, setUpper] = useState('');
  const [locked, setLocked] = useState('locked');
  const [agree, setAgree] = useState(false);
  const [hover, setHover] = useState(0);
  const [focusLog, setFocusLog] = useState('');
  const [stop, setStop] = useState(false);
  const [showRef, setShowRef] = useState(true);
  const [refLog, setRefLog] = useState('');
  const [layoutSaw, setLayoutSaw] = useState('');
  const inputRef = useRef<HTMLInputElement | null>(null);
  const counterRef = useRef<any>(null);
  const nativeRef = useRef<HTMLButtonElement | null>(null);

  const add = (s: string) => setLog((prev: string) => prev + s + ';');

  useLayoutEffect(() => {
    setLayoutSaw(inputRef.current ? inputRef.current.tagName : 'null');
  });

  useEffect(() => {
    const btn = nativeRef.current;
    const onNative = () => add('native-inner');
    const onDoc = () => add('native-document');
    if (btn) btn.addEventListener('click', onNative);
    document.addEventListener('click', onDoc);
    return () => {
      if (btn) btn.removeEventListener('click', onNative);
      document.removeEventListener('click', onDoc);
    };
  }, []);

  return (
    <div class="mx-auto max-w-2xl space-y-6 px-6 py-10">
      <h1 class="text-3xl font-bold text-slate-100">React events, forms and refs</h1>

      <section
        data-testid="outer"
        onClickCapture={() => add('outer-capture')}
        onClick={() => add('outer-bubble')}
      >
        <div onClickCapture={() => add('middle-capture')} onClick={() => add('middle-bubble')}>
          <button
            ref={nativeRef}
            data-testid="order-button"
            onClick={(e: any) => {
              add('button:' + e.type + ':' + (e.currentTarget === e.target ? 'self' : 'other'));
              if (stop) e.stopPropagation();
            }}
          >
            Click order
          </button>
        </div>
      </section>
      <label>
        <input data-testid="stop-toggle" type="checkbox" checked={stop} onChange={(e: any) => setStop(e.target.checked)} />
        stopPropagation
      </label>
      <button data-testid="clear-log" onClick={(e: any) => { e.stopPropagation(); setLog(''); }}>
        Clear
      </button>
      <pre data-testid="event-log">{log}</pre>

      <input data-testid="upper" value={upper} onChange={(e: any) => setUpper(e.target.value.toUpperCase())} />
      <input data-testid="locked" value={locked} onChange={() => {}} />
      <input data-testid="agree" type="checkbox" checked={agree} onChange={() => {}} />

      <div
        data-testid="hover-box"
        onMouseEnter={() => setHover((h: number) => h + 1)}
        onMouseLeave={() => setHover((h: number) => h + 10)}
      >
        hover me <span data-testid="hover-count">{hover}</span>
      </div>

      <div
        data-testid="focus-wrap"
        onFocus={(e: any) => setFocusLog((l: string) => l + 'focus:' + e.target.dataset.testid + ';')}
        onBlur={(e: any) => setFocusLog((l: string) => l + 'blur:' + e.target.dataset.testid + ';')}
      >
        <FocusInput ref={inputRef} data-testid="ref-input" placeholder="ref as prop" />
        <input data-testid="other-input" />
      </div>
      <pre data-testid="focus-log">{focusLog}</pre>
      <span data-testid="layout-saw">{layoutSaw}</span>
      <button data-testid="focus-ref" onClick={() => inputRef.current && inputRef.current.focus()}>
        Focus via ref
      </button>

      <Counter ref={counterRef} />
      <button data-testid="bump" onClick={() => counterRef.current && counterRef.current.bump()}>
        Bump via handle
      </button>

      <button data-testid="toggle-ref" onClick={() => setShowRef(!showRef)}>
        Toggle ref target
      </button>
      {showRef && (
        <em
          data-testid="ref-target"
          ref={(el: any) => {
            setRefLog((l: string) => l + 'attach:' + el.tagName + ';');
            return () => setRefLog((l: string) => l + 'cleanup;');
          }}
        >
          ref target
        </em>
      )}
      <pre data-testid="ref-log">{refLog}</pre>

      <form data-testid="reset-form">
        <input data-testid="uncontrolled" defaultValue="start" />
        <button type="reset" data-testid="reset">
          Reset
        </button>
      </form>
    </div>
  );
}
