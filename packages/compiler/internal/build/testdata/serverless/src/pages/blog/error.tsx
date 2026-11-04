export default function BlogError({ error }: any) {
  return (
    <div id="blog-error">
      <h1 id="page">blog-error</h1>
      <p id="m">{error.message}</p>
      <p id="d">{error.digest}</p>
    </div>
  );
}
