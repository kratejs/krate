import { useState } from "react";
import { log } from "../lib/log";

// onSubmit with preventDefault keeps the page and reads the fields through
// FormData; pressing Enter in a field submits too.
export default function EventSubmitPrevent() {
  const [result, setResult] = useState("");
  return (
    <form
      id="login"
      onSubmit={(e) => {
        e.preventDefault();
        const data = new FormData(e.currentTarget);
        log("submit", data.get("user"), data.get("remember"), e.defaultPrevented);
        setResult("welcome " + data.get("user"));
      }}
    >
      <input id="user" name="user" defaultValue="ada" />
      <label>
        <input id="remember" name="remember" type="checkbox" defaultChecked /> remember
      </label>
      <button id="go" type="submit">
        sign in
      </button>
      <p id="result">{result}</p>
    </form>
  );
}
