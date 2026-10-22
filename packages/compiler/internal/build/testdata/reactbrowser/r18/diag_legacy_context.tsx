import { Component } from "react";

class ThemedLabel extends Component<{ text: string }> {
  static contextTypes = { color: () => null };
  render() {
    return <span style={{ color: (this.context as { color: string }).color }}>{this.props.text}</span>;
  }
}

// Legacy context (childContextTypes/getChildContext/contextTypes) was
// removed in React 19. Krate must report it.
export default class DiagLegacyContext extends Component {
  static childContextTypes = { color: () => null };
  getChildContext() {
    return { color: "purple" };
  }
  render() {
    return <ThemedLabel text="legacy" />;
  }
}
