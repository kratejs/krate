"use client";
import { useEffect } from "react";

// Counts mounts so the browser test can tell a remounted template from a kept one.
export default function TemplateMark() {
  useEffect(() => {
    const w = window as any;
    w.__tplMounts = (w.__tplMounts || 0) + 1;
    document.getElementById("tpl")?.setAttribute("data-mounts", String(w.__tplMounts));
  }, []);
  return null;
}
