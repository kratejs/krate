import { useState, useTransition } from "react";
import { lookupUser } from "../actions/guestbook";
import { log } from "../lib/log";

// Calling a "use server" function directly from an event handler inside an
// async transition; the result comes back as data.
export default function ActionServerDirect() {
  const [user, setUser] = useState("nobody");
  const [isPending, startTransition] = useTransition();
  return (
    <div>
      <p id="user">{isPending ? "loading" : user}</p>
      <button
        id="load"
        onClick={() =>
          startTransition(async () => {
            const u = await lookupUser(2);
            log("looked up", u.id, u.name);
            startTransition(() => setUser(u.name));
          })
        }
      >
        load user 2
      </button>
    </div>
  );
}
