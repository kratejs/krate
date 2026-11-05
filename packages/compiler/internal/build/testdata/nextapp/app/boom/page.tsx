export default async function Boom() {
  const res = await fetch(`${process.env.DATA_URL}/api/posts`, { next: { tags: ["posts"] } });
  await res.json();
  throw new Error("secret server failure");
}
