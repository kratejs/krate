export default function Fixture() {
  return (
    <svg>
      <defs>
        <linearGradient id="g" x1="0" y1="0" x2="1" y2="1" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#fff" stopOpacity={0.5} />
          <stop offset="100%" stopColor="#000" />
        </linearGradient>
      </defs>
      <rect width="10" height="10" fill="url(#g)" />
    </svg>
  );
}
