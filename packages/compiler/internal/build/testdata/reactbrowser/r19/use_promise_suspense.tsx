import { Suspense, use, useState } from "react";
import { delay } from "../lib/log";

const cache = new Map<string, Promise<string[]>>();
function fetchComments(post: string) {
  if (!cache.has(post)) cache.set(post, delay(250, [post + ": first!", post + ": nice"]));
  return cache.get(post)!;
}

function Comments(props: { promise: Promise<string[]> }) {
  const comments = use(props.promise);
  return (
    <ul id="comments">
      {comments.map((c) => (
        <li key={c}>{c}</li>
      ))}
    </ul>
  );
}

// use(promise) suspends until the promise resolves; switching to a new
// promise shows the fallback again.
export default function UsePromiseSuspense() {
  const [post, setPost] = useState("intro");
  return (
    <div>
      <Suspense fallback={<p id="loading">loading comments</p>}>
        <Comments promise={fetchComments(post)} />
      </Suspense>
      <button id="next" onClick={() => setPost("part2")}>
        next post
      </button>
    </div>
  );
}
