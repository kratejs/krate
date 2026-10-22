import { Component } from "react";

// String refs were removed in React 19 (this.refs is gone). Krate must
// report them instead of silently dropping the ref.
export default class DiagStringRef extends Component {
  componentDidMount() {
    (this.refs as Record<string, HTMLInputElement>).name.focus();
  }
  render() {
    return <input ref="name" />;
  }
}
