"use client";
import { useState } from "react";

export default function LikeButton({ slug, initial }: { slug: string; initial: number }) {
  const [likes, setLikes] = useState(initial);
  return (
    <button id={"like-" + slug} onClick={() => setLikes(likes + 1)}>
      {likes} likes
    </button>
  );
}
