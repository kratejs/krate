import { createContext, use, useState } from "react";

const Theme = createContext("light");

function Banner(props: { show: boolean }) {
  if (!props.show) return <p id="banner">hidden</p>;
  // use() may be called conditionally, unlike useContext.
  const theme = use(Theme);
  return <p id="banner" className={theme}>banner in {theme}</p>;
}

// use(Context) reads context like useContext, including after an early
// return.
export default function UseContextFixture() {
  const [theme, setTheme] = useState("light");
  const [show, setShow] = useState(false);
  return (
    <Theme.Provider value={theme}>
      <Banner show={show} />
      <button id="show" onClick={() => setShow(true)}>
        show
      </button>
      <button id="dark" onClick={() => setTheme("dark")}>
        dark
      </button>
    </Theme.Provider>
  );
}
