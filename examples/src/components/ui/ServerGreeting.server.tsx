// @server
export default function ServerGreeting(props: { name: string }) {
  return (
    <div class="server-greeting">
      <h2>Hello, {props.name}!</h2>
      <p>This component was rendered at build time: {Date.now()}</p>
    </div>
  );
}
