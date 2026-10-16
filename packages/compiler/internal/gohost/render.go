package gohost

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Render invokes the host's `render` batch mode for the collected requests and
// returns a marker-id -> HTML map. The host binary must already be built.
func Render(root string, reqs []irtree.GoRenderRequest) (map[string]string, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	bin := HostBinaryPath(root)
	var in strings.Builder
	for _, r := range reqs {
		line, _ := json.Marshal(map[string]string{"id": r.ID, "name": r.Name, "props": r.Props})
		in.Write(line)
		in.WriteByte('\n')
	}
	cmd := exec.Command(bin, "render")
	cmd.Stdin = strings.NewReader(in.String())
	cmd.Env = os.Environ()
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Go host render failed: %v\n%s", err, errb.String())
	}
	res := make(map[string]string, len(reqs))
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<26)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var resp struct {
			ID    string
			HTML  string
			Error string
		}
		if json.Unmarshal([]byte(line), &resp) != nil {
			continue
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("Go component %q: %s", resp.ID, resp.Error)
		}
		res[resp.ID] = resp.HTML
	}
	return res, nil
}
