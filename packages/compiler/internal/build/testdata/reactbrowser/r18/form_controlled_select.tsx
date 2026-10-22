import { useState } from "react";
import { log } from "../lib/log";

const cities: Record<string, string[]> = {
  fr: ["Paris", "Lyon"],
  jp: ["Tokyo", "Osaka", "Kyoto"],
};

// A controlled select drives a dependent select whose options change.
export default function FormControlledSelect() {
  const [country, setCountry] = useState("fr");
  const [city, setCity] = useState("Lyon");
  return (
    <div>
      <select
        id="country"
        value={country}
        onChange={(e) => {
          log("country", e.target.value);
          setCountry(e.target.value);
          setCity(cities[e.target.value][0]);
        }}
      >
        <option value="fr">France</option>
        <option value="jp">Japan</option>
      </select>
      <select id="city" value={city} onChange={(e) => setCity(e.target.value)}>
        {cities[country].map((c) => (
          <option key={c} value={c}>
            {c}
          </option>
        ))}
      </select>
      <p id="choice">
        {city}, {country}
      </p>
    </div>
  );
}
