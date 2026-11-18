// The declaration `krate migrate nextjs` writes for a next-auth augmentation.
export {};

declare module "../../src/types.js" {
  interface SessionUserExtensions {
    role: "admin" | "member";
    credits: number;
    banned?: boolean;
  }
  interface UserExtensions {
    role: "admin" | "member";
  }
}
