import { Children, cloneElement, useState, type ReactElement, type ReactNode } from "react";

function Tab(props: { name: string; active?: boolean; onSelect?: () => void }) {
  return <button className={props.active ? "act" : ""} id={"tab-" + props.name} onClick={props.onSelect}>{props.name}</button>;
}

function Tabs(props: { children: ReactNode }) {
  const [active, setActive] = useState(0);
  return (
    <div id="tabs">
      {Children.map(props.children, (c, i) =>
        cloneElement(c as ReactElement<any>, { active: i === active, onSelect: () => setActive(i) }))}
      <p id="cur">{active}</p>
    </div>
  );
}

function Terms() {
  return [<dt key="t">Term</dt>, <dd key="d">Def</dd>];
}

export default function ChildrenMap() {
  return (
    <div>
      <Tabs>
        <Tab name="a" />
        <Tab name="b" />
        <Tab name="c" />
      </Tabs>
      <dl><Terms /></dl>
    </div>
  );
}
