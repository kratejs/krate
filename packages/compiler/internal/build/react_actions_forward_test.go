package build

import (
	"strings"
	"testing"
)

func TestFunctionActionForwardedToChild(t *testing.T) {
	html, js := buildActionsFixture(t, `import { useActionState } from "react";
import { addTodo } from "../actions/todos";

function Shell(props: { action: unknown }) {
  return <form action={props.action}><button>Go</button></form>;
}

export default function Page() {
  const [todos, formAction] = useActionState(addTodo, []);
  const local = async () => {};
  return <main><Shell action={formAction} /><Shell action={local} /><p>{todos.length}</p></main>;
}
`)
	if strings.Contains(html, `action="`) {
		t.Fatalf("forwarded function actions must not render an action attribute:\n%s", html)
	}
	for _, want := range []string{"={action:formAction}", "={action:local}", "__krateBindFormAction(el,function(){return (props.action);})"} {
		if !strings.Contains(js, want) {
			t.Fatalf("missing %q in:\n%s", want, js)
		}
	}
}
