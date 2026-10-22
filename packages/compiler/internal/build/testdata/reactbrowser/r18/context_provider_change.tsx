import { createContext, useContext, useEffect, useState } from "react";
import { log } from "../lib/log";

type Theme = { color: string; size: number };
const ThemeContext = createContext<Theme>({ color: "black", size: 12 });

function Label(props: { text: string }) {
  const theme = useContext(ThemeContext);
  useEffect(() => {
    log("label sees", theme.color, theme.size);
  }, [theme]);
  return (
    <span className="label" data-color={theme.color} style={{ fontSize: theme.size }}>
      {props.text}
    </span>
  );
}

function Toolbar() {
  return (
    <div className="toolbar">
      <Label text="save" />
      <Label text="load" />
    </div>
  );
}

// Consumers deep in the tree update when the provider value changes.
export default function ContextProviderChange() {
  const [theme, setTheme] = useState<Theme>({ color: "teal", size: 14 });
  return (
    <ThemeContext.Provider value={theme}>
      <Toolbar />
      <button id="red" onClick={() => setTheme({ ...theme, color: "red" })}>
        red
      </button>
      <button id="bigger" onClick={() => setTheme({ ...theme, size: theme.size + 2 })}>
        bigger
      </button>
    </ThemeContext.Provider>
  );
}
