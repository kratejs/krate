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
	os.WriteFile(filepath.Join(dir, "ServerComponent.tsx"), []byte(serverSrc), 0644)

	runtimeSrc := "// @runtime\nexport default function RuntimeComponent() { return <div>runtime</div>; }"
	os.WriteFile(filepath.Join(dir, "RuntimeComponent.tsx"), []byte(runtimeSrc), 0644)

	clientSrc := "export default function ClientComponent() { return <div>client</div>; }"
	os.WriteFile(filepath.Join(dir, "ClientComponent.tsx"), []byte(clientSrc), 0644)

	b := New(dir)
	return dir, b
}

func TestCompositionRules_ClientImportsServer(t *testing.T) {
	dir, b := setupCompositionTest(t)

	entry := filepath.Join(dir, "entry.tsx")
	src := "import ServerComponent from './ServerComponent';\nexport default function App() { return <ServerComponent/>; }"
	os.WriteFile(entry, []byte(src), 0644)

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

func TestCompositionRules_ClientImportsRuntime(t *testing.T) {
	dir, b := setupCompositionTest(t)

	entry := filepath.Join(dir, "entry.tsx")
	src := "import RuntimeComponent from './RuntimeComponent';\nexport default function App() { return <RuntimeComponent/>; }"
	os.WriteFile(entry, []byte(src), 0644)

	_, err := b.Bundle(entry)
	if err == nil {
		t.Fatal("expected composition error for client importing runtime")
	}

	var compErr *CompositionError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositionError, got %T: %v", err, err)
	}

	if !strings.Contains(compErr.ImportedClass, "runtime") {
		t.Errorf("expected 'runtime' in ImportedClass, got %q", compErr.ImportedClass)
	}
}

func TestCompositionRules_ServerImportsClient(t *testing.T) {
	dir, b := setupCompositionTest(t)

	entry := filepath.Join(dir, "entry.tsx")
	src := "// @server\nimport ClientComponent from './ClientComponent';\nexport default function App() { return <ClientComponent/>; }"
	os.WriteFile(entry, []byte(src), 0644)

	_, err := b.Bundle(entry)
	if err != nil {
		t.Fatalf("server importing client should be allowed, got error: %v", err)
	}
}

func TestCompositionRules_RuntimeImportsBoth(t *testing.T) {
	dir, b := setupCompositionTest(t)

	entry := filepath.Join(dir, "entry.tsx")
	src := "// @runtime\nimport ServerComponent from './ServerComponent';\nimport ClientComponent from './ClientComponent';\nexport default function App() { return <div><ServerComponent/><ClientComponent/></div>; }"
	os.WriteFile(entry, []byte(src), 0644)

	_, err := b.Bundle(entry)
	if err != nil {
		t.Fatalf("runtime importing both should be allowed, got error: %v", err)
	}
}

func TestCompositionRules_ClientImportsClient(t *testing.T) {
	dir, b := setupCompositionTest(t)

	otherClient := "export default function Other() { return <span>hi</span>; }"
	os.WriteFile(filepath.Join(dir, "Other.tsx"), []byte(otherClient), 0644)

	entry := filepath.Join(dir, "entry.tsx")
	src := "import Other from './Other';\nexport default function App() { return <Other/>; }"
	os.WriteFile(entry, []byte(src), 0644)

	_, err := b.Bundle(entry)
	if err != nil {
		t.Fatalf("client importing client should be allowed, got error: %v", err)
	}
}
