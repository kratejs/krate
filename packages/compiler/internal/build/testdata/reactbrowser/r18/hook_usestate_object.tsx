import { useState } from "react";

type Profile = { name: string; tags: string[]; address: { city: string } };

// Immutable updates of nested object and array state.
export default function HookUseStateObject() {
  const [profile, setProfile] = useState<Profile>({ name: "Ada", tags: ["math"], address: { city: "London" } });
  return (
    <div>
      <h2 id="name">{profile.name}</h2>
      <p id="city">{profile.address.city}</p>
      <ul id="tags">
        {profile.tags.map((t) => (
          <li key={t}>{t}</li>
        ))}
      </ul>
      <input id="name-input" value={profile.name} onChange={(e) => setProfile({ ...profile, name: e.target.value })} />
      <button id="move" onClick={() => setProfile({ ...profile, address: { ...profile.address, city: "Paris" } })}>
        move
      </button>
      <button id="tag" onClick={() => setProfile((p) => ({ ...p, tags: [...p.tags, "code" + p.tags.length] }))}>
        tag
      </button>
      <button id="untag" onClick={() => setProfile((p) => ({ ...p, tags: p.tags.slice(1) }))}>
        untag
      </button>
    </div>
  );
}
