import { Component, createContext, useState } from "react";

const Locale = createContext("en");

class Greeting extends Component<{ name: string }> {
  static contextType = Locale;
  declare context: string;

  render() {
    const hello = this.context === "fr" ? "Bonjour" : this.context === "de" ? "Hallo" : "Hello";
    return (
      <p className="greeting">
        {hello}, {this.props.name}
      </p>
    );
  }
}

// static contextType gives a class component this.context, updated when the
// provider value changes.
export default function ClassContextType() {
  const [locale, setLocale] = useState("en");
  return (
    <div>
      <Greeting name="outside" />
      <Locale.Provider value={locale}>
        <Greeting name="inside" />
      </Locale.Provider>
      <button id="fr" onClick={() => setLocale("fr")}>
        fr
      </button>
      <button id="de" onClick={() => setLocale("de")}>
        de
      </button>
    </div>
  );
}
