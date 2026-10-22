import { Component, useState } from "react";
import { log } from "../lib/log";

class ErrorBoundary extends Component<{ children?: any }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    log("gDSFE", error.message);
    return { error };
  }

  render() {
    if (this.state.error) {
      return <p id="fallback">Something went wrong: {this.state.error.message}</p>;
    }
    return this.props.children;
  }
}

function Profile(props: { user: { name: string } | null }) {
  if (!props.user) throw new Error("no user loaded");
  return <p id="profile">{props.user.name}</p>;
}

// A render error in a child replaces the boundary subtree with the fallback;
// siblings outside the boundary keep working.
export default function BoundaryRenderError() {
  const [user, setUser] = useState<{ name: string } | null>({ name: "Ada" });
  const [n, setN] = useState(0);
  return (
    <div>
      <ErrorBoundary>
        <Profile user={user} />
      </ErrorBoundary>
      <p id="n">outside {n}</p>
      <button id="break" onClick={() => setUser(null)}>
        log out
      </button>
      <button id="outside" onClick={() => setN(n + 1)}>
        outside
      </button>
    </div>
  );
}
