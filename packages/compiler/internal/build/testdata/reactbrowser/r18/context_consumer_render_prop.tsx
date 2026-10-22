import { createContext, useState } from "react";

const Auth = createContext<{ user: string | null; login: () => void }>({ user: null, login: () => {} });

// Context.Consumer with a render-prop child, including a function from the
// context value that updates the provider's state.
export default function ContextConsumerRenderProp() {
  const [user, setUser] = useState<string | null>(null);
  return (
    <Auth.Provider value={{ user, login: () => setUser("ada") }}>
      <header>
        <Auth.Consumer>
          {(auth) =>
            auth.user ? (
              <span id="hello">hello {auth.user}</span>
            ) : (
              <button id="login" onClick={auth.login}>
                log in
              </button>
            )
          }
        </Auth.Consumer>
      </header>
    </Auth.Provider>
  );
}
