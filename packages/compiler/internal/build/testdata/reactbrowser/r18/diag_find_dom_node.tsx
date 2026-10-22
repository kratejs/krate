import { Component } from "react";
import { findDOMNode } from "react-dom";

// findDOMNode was removed in React 19. Krate must report it with a hint to
// use a ref.
export default class DiagFindDomNode extends Component {
  componentDidMount() {
    const node = findDOMNode(this) as HTMLElement | null;
    node?.classList.add("mounted");
  }
  render() {
    return <div>legacy</div>;
  }
}
