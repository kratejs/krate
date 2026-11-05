package build

import (
	"fmt"
	"testing"
)

// Island detection runs before a page's universal transforms, so it can read a
// module another page is transforming. Only meaningful under -race, which CI
// runs.
func TestConcurrentPagesShareModuleDuringTransform(t *testing.T) {
	files := map[string]string{
		"icons/menu.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M4 6h16"/></svg>`,
		"src/shared.tsx": `const mark = <Icon name="menu" />;
const wrap = (x: unknown) => x;
const made = wrap(<Icon name="menu" />);
export function Shared() { return <div>{mark}{made}<Icon name="menu" /></div>; }`,
	}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("src/pages/p%d.tsx", i)] = `import { Shared } from "../shared";
export default function Page() { return <Shared />; }`
	}
	for i := 0; i < 5; i++ {
		_, _, logged := buildFiles(t, files, nil)
		if logged == "" {
			t.Fatal("expected build output")
		}
	}
}
