package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestHeadScriptInlineEscaped(t *testing.T) {
	got := renderHeadScript(config.HeadScript{Content: "var x='</script>';"})
	if !strings.Contains(got, `<\/script>`) {
		t.Errorf("expected escaped terminator, got:\n%s", got)
	}
	if strings.Contains(got, `var x='</script>'`) {
		t.Errorf("unescaped inline terminator present:\n%s", got)
	}
}
