package build

import (
	"os/exec"
	"runtime"
	"strings"
)

// devEditor is an editor the overlay may open files in. ID is the command
// looked up on PATH.
type devEditor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// devEditors are the GUI editors the overlay offers, when found on PATH.
// Terminal editors are left out: the dev server has no terminal to run them
// in.
var devEditors = []devEditor{
	{"code", "VS Code"},
	{"code-insiders", "VS Code Insiders"},
	{"cursor", "Cursor"},
	{"windsurf", "Windsurf"},
	{"codium", "VSCodium"},
	{"trae", "Trae"},
	{"zed", "Zed"},
	{"subl", "Sublime Text"},
	{"webstorm", "WebStorm"},
	{"idea", "IntelliJ IDEA"},
	{"phpstorm", "PhpStorm"},
	{"pycharm", "PyCharm"},
	{"goland", "GoLand"},
	{"rider", "Rider"},
	{"rubymine", "RubyMine"},
	{"clion", "CLion"},
	{"studio", "Android Studio"},
	{"mate", "TextMate"},
}

// lookEditor resolves an editor command on PATH; tests replace it.
var lookEditor = func(id string) (string, error) {
	path, err := exec.LookPath(id)
	if err != nil && runtime.GOOS == "windows" {
		// Editors install .cmd launchers on Windows.
		path, err = exec.LookPath(id + ".cmd")
	}
	return path, err
}

// availableEditors lists the allowlisted editors found on PATH.
func availableEditors() []devEditor {
	out := []devEditor{}
	for _, e := range devEditors {
		if _, err := lookEditor(e.ID); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// allowedEditor reports whether id names an allowlisted editor that resolves
// on PATH.
func allowedEditor(id string) bool {
	id = strings.TrimSpace(id)
	for _, e := range devEditors {
		if e.ID == id {
			_, err := lookEditor(id)
			return err == nil
		}
	}
	return false
}
