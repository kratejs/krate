import { createContext, useContext, useState } from "react";

const Depth = createContext(0);
const User = createContext("anonymous");

function Level(props: { children?: any }) {
  const depth = useContext(Depth);
  const user = useContext(User);
  return (
    <Depth.Provider value={depth + 1}>
      <div className="level" data-depth={depth + 1}>
        depth {depth + 1} for {user}
        {props.children}
      </div>
    </Depth.Provider>
  );
}

// The nearest provider wins; nested providers of the same context shadow
// outer ones, and providers of different contexts compose.
export default function ContextNestedProviders() {
  const [user, setUser] = useState("ada");
  return (
    <User.Provider value={user}>
      <Level>
        <Level>
          <User.Provider value="guest">
            <Level />
          </User.Provider>
        </Level>
      </Level>
      <button id="switch" onClick={() => setUser("grace")}>
        switch user
      </button>
    </User.Provider>
  );
}
