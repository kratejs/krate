import { useCallback, useEffect, useState } from "react";
import { log } from "../lib/log";

// Functions in dependency arrays: an inline function changes every render,
// a useCallback function only when its own dependencies change.
export default function EffectDepsFunction() {
  const [userId, setUserId] = useState(1);
  const [unrelated, setUnrelated] = useState(0);

  const inlineFetch = () => "user-" + userId;
  const stableFetch = useCallback(() => "user-" + userId, [userId]);

  useEffect(() => {
    log("inline fetch", inlineFetch());
  }, [inlineFetch]);

  useEffect(() => {
    log("stable fetch", stableFetch());
  }, [stableFetch]);

  return (
    <div>
      <p id="out">
        {userId}:{unrelated}
      </p>
      <button id="user" onClick={() => setUserId(userId + 1)}>
        next user
      </button>
      <button id="unrelated" onClick={() => setUnrelated(unrelated + 1)}>
        unrelated
      </button>
    </div>
  );
}
