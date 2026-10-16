import './skeleton.css';

export interface SkeletonProps {
  class?: string;
}

// Skeleton is a static shimmer placeholder. Animation is pure CSS, so the
// component ships no client JS.
export function Skeleton(props: SkeletonProps) {
  var className = "krate-skeleton";
  if (props.class) className += " " + props.class;
  return <div class={className} aria-hidden="true" />;
}
