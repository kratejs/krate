"use client";
import { addPost } from "../actions";

export default function AddPost() {
  return (
    <button id="add-post" onClick={async () => { await addPost("Third post"); }}>
      Add post
    </button>
  );
}
