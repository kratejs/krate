package build

import (
	"strings"
	"testing"
)

// headAndBody splits a built page at </head>.
func headAndBody(t *testing.T, html string) (string, string) {
	t.Helper()
	i := strings.Index(html, "</head>")
	if i < 0 {
		t.Fatalf("no </head> in:\n%s", html)
	}
	return html[:i], html[i:]
}

func TestMetadataHoistedFromStaticComponent(t *testing.T) {
	html, _ := buildCompatSrc(t, `function Seo(props: { title: string }) {
  return (
    <>
      <title>{props.title + " | Site"}</title>
      <meta name="description" content="about us" />
      <link rel="canonical" href="https://example.com/about" />
      <link rel="stylesheet" href="/a.css" precedence="default" />
      <link rel="stylesheet" href="/a.css" precedence="default" />
      <link rel="stylesheet" href="/plain.css" />
    </>
  );
}
export default function Page() {
  return (
    <main>
      <Seo title="About" />
      <svg viewBox="0 0 10 10"><title>icon</title></svg>
      <p itemScope><meta itemProp="name" content="x" /></p>
    </main>
  );
}`, nil)
	head, body := headAndBody(t, html)
	for _, want := range []string{"<title>About | Site</title>", `name=description`, `rel=canonical`, `href=/a.css`} {
		if !strings.Contains(head, want) {
			t.Errorf("head should contain %q:\n%s", want, head)
		}
	}
	if strings.Contains(head, "Krate App") {
		t.Errorf("a component title replaces the default title:\n%s", head)
	}
	if n := strings.Count(head, "href=/a.css"); n != 1 {
		t.Errorf("identical stylesheet should be hoisted once, got %d:\n%s", n, head)
	}
	if strings.Contains(head, "precedence") {
		t.Errorf("precedence is consumed by the hoist:\n%s", head)
	}
	for _, want := range []string{"<svg", "<title>icon</title>", "itemprop=name", "/plain.css"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should keep %q:\n%s", want, body)
		}
	}
}

func TestMetadataReactiveTitleBindsTextContent(t *testing.T) {
	html, outDir := buildCompatSrc(t, `import { useState } from "react";
function Inbox(props: { unread: number }) {
  return (
    <section>
      <title>{"Inbox (" + props.unread + ")"}</title>
      <p>{props.unread} unread</p>
    </section>
  );
}
export default function Page() {
  const [unread, setUnread] = useState(3);
  return <div><Inbox unread={unread} /><meta name="description" content={"unread " + unread} /><button onClick={() => setUnread(unread - 1)}>read</button></div>;
}`, nil)
	head, body := headAndBody(t, html)
	if !strings.Contains(head, ">Inbox (3)</title>") || strings.Contains(head, "<!--") {
		t.Errorf("title should be hoisted as plain text:\n%s", head)
	}
	if !strings.Contains(head, `content="unread 3"`) {
		t.Errorf("meta should be hoisted with its initial value:\n%s", head)
	}
	if strings.Contains(body, "<title") || strings.Contains(body, "<meta") {
		t.Errorf("metadata must not stay in the body:\n%s", body)
	}
	js := pageJS(t, outDir)
	if !strings.Contains(js, "kbindTextContent(") {
		t.Errorf("reactive title should bind its text:\n%s", js)
	}
	if !strings.Contains(js, `kbindAttr("`) {
		t.Errorf("reactive meta should bind its attribute:\n%s", js)
	}
	if !strings.Contains(js, `var props=__krate_props[`) && !strings.Contains(js, `=__krate_props["`) {
		t.Errorf("Inbox reads its props registry:\n%s", js)
	}
}

func TestMetadataInHeadAndBranchesStaysPut(t *testing.T) {
	html, _ := buildCompatSrc(t, `import { useState } from "react";
export default function Page() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <Head><title>From Head</title></Head>
      {open && <meta name="flag" content="open" />}
      <button onClick={() => setOpen(true)}>open</button>
    </div>
  );
}`, nil)
	head, _ := headAndBody(t, html)
	if strings.Count(head, "<title>From Head</title>") != 1 {
		t.Errorf("<Head> title should appear once:\n%s", head)
	}
	if strings.Contains(head, "name=flag") {
		t.Errorf("conditional metadata is not hoisted:\n%s", head)
	}
}
