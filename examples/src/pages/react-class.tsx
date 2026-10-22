// React class components on Krate: lifecycles in React order, error
// boundaries (runtime and build time), and class instance refs. The browser
// conformance suite drives this page (internal/build/testdata/browser_conformance.mjs).
import React, { Component, PureComponent, createRef } from 'react';

function record(entry: string) {
  const w = window as unknown as { __classLog?: string[] };
  (w.__classLog = w.__classLog || []).push(entry);
  const out = document.querySelector('[data-testid=class-log]');
  if (out) out.textContent = w.__classLog.join(';') + ';';
}

class Child extends Component<{ n: number; color?: string }> {
  static defaultProps = { color: 'teal' };
  componentDidMount() {
    record('child mount ' + this.props.n);
  }
  componentDidUpdate(prevProps: { n: number }) {
    record('child update ' + prevProps.n + '>' + this.props.n);
  }
  componentWillUnmount() {
    record('child unmount');
  }
  render() {
    return <span data-testid="child" class={this.props.color}>child {this.props.n}</span>;
  }
}

class Gate extends PureComponent<{ n: number }> {
  shouldComponentUpdate(next: { n: number }) {
    return next.n % 2 === 0;
  }
  render() {
    return <span data-testid="gate">even {this.props.n}</span>;
  }
}

class Lifecycle extends Component<{}, { n: number; show: boolean }> {
  state = { n: 1, show: true };
  child = createRef<Child>();
  static getDerivedStateFromProps(_props: {}, state: { n: number }) {
    return { doubled: state.n * 2 };
  }
  getSnapshotBeforeUpdate() {
    return document.querySelector('[data-testid=count]')?.textContent;
  }
  componentDidMount() {
    record('parent mount ref=' + (this.child.current ? 'instance' : 'null'));
  }
  componentDidUpdate(_pp: {}, prevState: { n: number }, snapshot: string) {
    record('parent update ' + prevState.n + '>' + this.state.n + ' was=' + snapshot);
  }
  inc = () => this.setState((s) => ({ n: s.n + 1 }), () => record('callback ' + this.state.n));
  render() {
    const { n, show } = this.state;
    return (
      <div>
        <p data-testid="count">n={n} doubled={(this.state as { doubled?: number }).doubled}</p>
        <button data-testid="inc" onClick={this.inc}>increment</button>
        <button data-testid="toggle" onClick={() => this.setState({ show: !show })}>toggle child</button>
        {show ? <Child n={n} ref={this.child} /> : <em>hidden</em>}
        <Gate n={n} />
      </div>
    );
  }
}

class ErrorBoundary extends Component<{ name: string; children?: unknown }, { error: string | null }> {
  state = { error: null };
  static getDerivedStateFromError(error: Error) {
    return { error: error.message };
  }
  componentDidCatch(error: Error, info: { componentStack: string }) {
    record('caught ' + this.props.name + ': ' + error.message + ' ' + info.componentStack.trim().split('\n')[0].trim());
  }
  render() {
    if (this.state.error) {
      return (
        <div data-testid={'fallback-' + this.props.name}>
          <p>Something went wrong: {this.state.error}</p>
          <button data-testid={'retry-' + this.props.name} onClick={() => this.setState({ error: null })}>try again</button>
        </div>
      );
    }
    return this.props.children;
  }
}

class BuggyCounter extends Component<{ id: string }, { counter: number }> {
  state = { counter: 0 };
  handleClick = () => this.setState(({ counter }) => ({ counter: counter + 1 }));
  render() {
    if (this.state.counter === 2) {
      throw new Error('crashed ' + this.props.id);
    }
    return <button data-testid={'buggy-' + this.props.id} onClick={this.handleClick}>{this.state.counter}</button>;
  }
}

function NeedsData(props: { data?: string }) {
  if (!props.data) throw new Error('no data at build time');
  return <p>{props.data}</p>;
}

class HandlerThrows extends Component {
  render() {
    return (
      <button data-testid="handler-throws" onClick={() => { throw new Error('handler error'); }}>
        throw in a handler
      </button>
    );
  }
}

export default function ReactClassPage() {
  return (
    <main>
      <h1>React class components</h1>
      <section>
        <h2>Lifecycles</h2>
        <Lifecycle />
        <pre data-testid="class-log"></pre>
      </section>
      <section>
        <h2>Error boundaries</h2>
        <ErrorBoundary name="outer">
          <ErrorBoundary name="inner">
            <BuggyCounter id="a" />
          </ErrorBoundary>
          <BuggyCounter id="b" />
        </ErrorBoundary>
        <ErrorBoundary name="build">
          <NeedsData />
        </ErrorBoundary>
        <ErrorBoundary name="handler">
          <HandlerThrows />
        </ErrorBoundary>
      </section>
    </main>
  );
}
