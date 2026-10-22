package build

import (
	"strings"
	"testing"
)

func TestCreatePortalLowersToAnchor(t *testing.T) {
	html, outDir := buildCompatSrc(t, `import { useState } from "react";
import { createPortal } from "react-dom";
function Modal(props: { onClose: () => void }) {
  return createPortal(<div id="backdrop"><button id="close" onClick={props.onClose}>close</button></div>, document.getElementById("modal-root"));
}
export default function Page() {
  const [count, setCount] = useState(0);
  const [open, setOpen] = useState(false);
  return (
    <div id="host">
      {createPortal(<button id="toast" onClick={() => setCount(count + 1)}>toast {count}</button>, document.body)}
      <button id="open" onClick={() => setOpen(true)}>open</button>
      {open && <Modal onClose={() => setOpen(false)} />}
    </div>
  );
}`, nil)
	if !strings.Contains(html, `<template data-krate-portal=`) {
		t.Fatalf("portal should render an anchor template:\n%s", html)
	}
	if !strings.Contains(html, `data-krate-portal-target="#modal-root"`) && !strings.Contains(html, `data-krate-portal-target=#modal-root`) {
		t.Errorf("getElementById container should become a selector:\n%s", html)
	}
	if !strings.Contains(html, `id=toast`) || !strings.Contains(html, `id=close`) {
		t.Errorf("portal content should be built:\n%s", html)
	}
	js := pageJS(t, outDir)
	if strings.Contains(js, "createPortal(") {
		t.Errorf("createPortal must be lowered at build time:\n%s", js)
	}
	if i, j := strings.LastIndex(js, "krateMountPortals"), strings.LastIndex(js, `kbindHandler(`); i < 0 || i < j {
		t.Errorf("portals must mount after the bindings are installed:\n%s", js)
	}
}

func TestCreatePortalDynamicContainer(t *testing.T) {
	_, outDir := buildCompatSrc(t, `import { useState } from "react";
import { createPortal } from "react-dom";
export default function Page() {
  const [n, setN] = useState(0);
  const el = document.querySelector("main") || document.body;
  return <div>{createPortal(<p onClick={() => setN(n + 1)}>{n}</p>, el)}</div>;
}`, nil)
	js := pageJS(t, outDir)
	if !strings.Contains(js, "kratePortalTarget(") {
		t.Errorf("a non-literal container should be registered from the hydration scope:\n%s", js)
	}
}
