'use client';
// Loads the viewer lazily once mounted, with no Suspense boundary of its own:
// the island root is the boundary, as the React root is.
import { lazy, useEffect, useState } from "react";

const ViewerLazy = lazy(() => import("./viewer"));

export function LazyViewer(props: { url: string }) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => {
    setMounted(true);
  }, []);
  return mounted ? <ViewerLazy {...props} /> : <p id="viewer-loading">loading</p>;
}
