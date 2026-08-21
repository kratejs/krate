let contextId = 0;

export interface Context<T> {
  id: number;
  defaultValue: T;
  Provider: (props: { value: T; children?: unknown[] }) => Node;
  useContext: () => T;
}

const contextStack: Map<number, unknown[]> = new Map();

// Reset all context stacks to their default values.
// Called during SPA navigation to prevent stale context leaking across pages.
export function resetContexts(): void {
  for (const [id, stack] of contextStack) {
    if (stack.length > 0) {
      const defaultVal = stack[0];
      contextStack.set(id, [defaultVal]);
    }
  }
}

export function createContext<T>(defaultValue: T): Context<T> {
  const id = contextId++;
  contextStack.set(id, [defaultValue]);

  return {
    id,
    defaultValue,
    Provider: (props: { value: T; children?: unknown[] }): Node => {
      const stack = contextStack.get(id) || [defaultValue];
      stack.push(props.value);
      contextStack.set(id, stack);

      // Create a container and render children
      const container = document.createElement('div');
      container.style.display = 'contents';
      if (props.children) {
        for (const child of props.children) {
          if (child instanceof Node) {
            container.appendChild(child);
          } else if (child != null) {
            container.appendChild(document.createTextNode(String(child)));
          }
        }
      }

      // Pop the context value synchronously after rendering children.
      // Children are already rendered (passed as pre-built DOM nodes), so
      // any effects that read context have already done so during their
      // synchronous creation. Synchronous pop fixes the nested Provider race:
      // with microtask pop, code after an inner Provider but within the outer
      // Provider's scope would incorrectly see the inner value still on the stack.
      stack.pop();
      if (stack.length === 0) {
        stack.push(defaultValue);
      }

      return container;
    },
    useContext: (): T => {
      const stack = contextStack.get(id);
      if (stack && stack.length > 0) {
        return stack[stack.length - 1] as T;
      }
      return defaultValue;
    },
  };
}