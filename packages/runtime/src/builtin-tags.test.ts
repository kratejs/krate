import { describe, expect, it } from "vitest";
import * as runtime from "./index.js";

describe("compiler-provided tags", () => {
  it("are real exports of the runtime entry", () => {
    for (const name of ["Head", "Link", "Image", "Script", "Style", "Icon", "Suspense", "SyntaxHighlight"] as const) {
      expect(typeof runtime[name], name).toBe("function");
    }
  });

  it("render nothing for head content in the client entry", () => {
    expect((runtime.Head as unknown as (p: object) => unknown)({ children: "x" })).toBeNull();
  });
});
