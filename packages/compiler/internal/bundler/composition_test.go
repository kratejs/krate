package bundler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupCompositionTest(t *testing.T) (string, *Bundler) {
	t.Helper()
	dir := t.TempDir()

	serverSrc := "// @server\nexport default function ServerComponent() { return <div>server</div>; }"
	_ = os.WriteFile(filepath.Join(dir, "ServerComponent.tsx"), []byte(serverSrc), 0644)

	runtimeSrc := "// @runtime\nexport default function RuntimeComponent() { return <div>runtime</div>; }"
	_ = os.WriteFile(filepath.Join(dir, "RuntimeComponent.tsx"), []byte(runtimeSrc), 0644)

	clientSrc := "export default function ClientComponent() { return <div>client</div>; }"
	_ = os.WriteFile(filepath.Join(dir, "ClientComponent.tsx"), []byte(clientSrc), 0644)

	b := New(dir)
	return dir, b
}

func TestCompositionRules_ClientImportsServer(t *testing.T) {
	dir, b := setupCompositionTest(t)

	entry := filepath.Join(dir, "entry.tsx")
	src := "import ServerComponent from './ServerComponent';\nexport default function App() { return <ServerComponent/>; }"
	_ = os.WriteFile(entry, []byte(src), 0644)

	_, err := b.Bundle(entry)
	if err == nil {
		t.Fatal("expected composition error for client importing server")
	}

	var compErr *CompositionError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositionError, got %T: %v", err, err)
	}

	if !strings.Contains(compErr.ImportedClass, "server") {
		t.Errorf("expected 'server' in ImportedClass, got %q", compErr.ImportedClass)
	}
	if !strings.Contains(err.Error(), "cannot import") {
		t.Errorf("error message should mention 'cannot import', got: %s", err.Error())
	}
}

func TestCompositionRules_ServerImportsClient(t *testing.T) {
	dir, b := setupCompositionTest(t)

	entry := filepath.Join(dir, "entry.tsx")
	src := "// @server\nimport ClientComponent from './ClientComponent';\nexport default function App() { return <ClientComponent/>; }"
	_ = os.WriteFile(entry, []byte(src), 0644)

	_, err := b.Bundle(entry)
	if err != nil {
		t.Fatalf("server importing client should be allowed, got error: %v", err)
	}
}
