// The load snapshot is taken after the data and React's reveal throttle have
// settled (settleMs 900 in the steps file); a snapshot right after load raced
// the timer, so React and Krate could be caught in different states.
import { Suspense, use, useState } from "react";
import { createGate } from "../lib/log";

// Each post's comments resolve when the release button is clicked, not after a
// timer, so the fallback is stable instead of racing React's reveal throttle.
const gates = new Map<string, ReturnType<typeof createGate>>();
const cache = new Map<string, Promise<string[]>>();
function gateFor(post: string) {
  if (!gates.has(post)) gates.set(post, createGate());
  return gates.get(post)!;
}
function fetchComments(post: string) {
  if (!cache.has(post)) cache.set(post, gateFor(post).promise.then(() => [post + ": first!", post + ": nice"]));
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
      <button id="release" onClick={() => gateFor(post).open()}>
        load comments
      </button>
      <button id="next" onClick={() => setPost("part2")}>
        next post
      </button>
    </div>
  );
}
