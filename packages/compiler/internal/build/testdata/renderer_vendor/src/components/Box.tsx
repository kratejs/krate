function Skin(props: { tone: string }) {
  return <meshBasicMaterial color={props.tone} attach={(parent) => parent} />;
}

export function Box(props: { x: number }) {
  return <mesh position-x={props.x}><sphereGeometry /><Skin tone="red" /></mesh>;
}
