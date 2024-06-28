package devconfig

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"github.com/tailscale/hujson"
	"go.jetpack.io/devbox/internal/devconfig/configfile"
)

func TestOpen(t *testing.T) {
	t.Run("Dir", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		cfg, err := Open(root)
		if err != nil {
			t.Fatalf("Open(%q) error: %v", root, err)
		}
		gotDir := filepath.Dir(cfg.Root.AbsRootPath)
		if gotDir != root {
			t.Errorf("filepath.Dir(cfg.Root.AbsRootPath) = %q, want %q", gotDir, root)
		}
	})
	t.Run("File", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}
		path := filepath.Join(root, "devbox.json")

		cfg, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%q) error: %v", path, err)
		}
		gotDir := filepath.Dir(cfg.Root.AbsRootPath)
		if gotDir != root {
			t.Errorf("filepath.Dir(cfg.Root.AbsRootPath) = %q, want %q", gotDir, root)
		}
	})
}

func TestOpenError(t *testing.T) {
	t.Run("NotExist", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		path := filepath.Join(root, "notafile.json")
		cfg, err := Open(path)
		if err == nil {
			t.Fatalf("Open(%q) = %q, want error", root, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Error("errors.Is(err, fs.ErrNotExist) = false, want true")
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("errors.Is(err, ErrNotFound) = true, want false")
		}
	})
	t.Run("NotFound", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)

		cfg, err := Open(root)
		if err == nil {
			t.Fatalf("Open(%q) = %q, want error", root, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Error("errors.Is(err, ErrNotFound) = false, want true")
		}
	})
	t.Run("ParentNotFound", func(t *testing.T) {
		root, child, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		cfg, err := Open(child)
		if err == nil {
			t.Fatalf("Open(%q) = %q, want error", root, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Error("errors.Is(err, ErrNotFound) = false, want true")
		}
	})
}

func TestFind(t *testing.T) {
	t.Run("StartInSameDir", func(t *testing.T) {
		root, child, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}
		if _, err := Init(child); err != nil {
			t.Fatalf("Init(%q) error: %v", child, err)
		}

		cfg, err := Find(child)
		if err != nil {
			t.Fatalf("Find(%q) error: %v", child, err)
		}
		gotDir := filepath.Dir(cfg.Root.AbsRootPath)
		if gotDir != child {
			t.Errorf("filepath.Dir(cfg.Root.AbsRootPath) = %q, want %q", gotDir, child)
		}
	})
	t.Run("StartInChildDir", func(t *testing.T) {
		root, child, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		cfg, err := Find(child)
		if err != nil {
			t.Fatalf("Find(%q) error: %v", child, err)
		}
		gotDir := filepath.Dir(cfg.Root.AbsRootPath)
		if gotDir != root {
			t.Errorf("filepath.Dir(cfg.Root.AbsRootPath) = %q, want %q", gotDir, root)
		}
	})
	t.Run("StartInNestedChildDir", func(t *testing.T) {
		root, child, nested := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}
		if _, err := Init(child); err != nil {
			t.Fatalf("Init(%q) error: %v", child, err)
		}

		cfg, err := Find(nested)
		if err != nil {
			t.Fatalf("Find(%q) error: %v", nested, err)
		}
		gotDir := filepath.Dir(cfg.Root.AbsRootPath)
		if gotDir != child {
			t.Errorf("filepath.Dir(cfg.Root.AbsRootPath) = %q, want %q", gotDir, child)
		}
	})
	t.Run("IgnoreDirsWithMatchingName", func(t *testing.T) {
		root, child, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		trickyDir := filepath.Join(child, "devbox.json")
		perm := fs.FileMode(0o777)
		if err := os.Mkdir(trickyDir, perm); err != nil {
			t.Fatalf("Mkdir(%q, %O) error: %v", trickyDir, perm, err)
		}

		cfg, err := Find(child)
		if errors.Is(err, errIsDirectory) {
			t.Fatalf("Find(%q) did not ignore a directory named devbox.json: %v", child, err)
		}
		if err != nil {
			t.Fatalf("Find(%q) error: %v", child, err)
		}
		gotDir := filepath.Dir(cfg.Root.AbsRootPath)
		if gotDir != root {
			t.Errorf("filepath.Dir(cfg.Root.AbsRootPath) = %q, want %q", gotDir, root)
		}
	})
	t.Run("ExactFile", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		path := filepath.Join(root, "devbox.json")
		cfg, err := Find(path)
		if err != nil {
			t.Fatalf("Find(%q) error: %v", path, err)
		}
		if cfg.Root.AbsRootPath != path {
			t.Errorf("cfg.Root.AbsRootPath = %q, want %q", cfg.Root.AbsRootPath, path)
		}
	})
}

func TestFindError(t *testing.T) {
	t.Run("NotExist", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		path := filepath.Join(root, "notafile.json")
		cfg, err := Find(path)
		if err == nil {
			t.Fatalf("Find(%q) = %q, want error", path, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Error("errors.Is(err, fs.ErrNotExist) = false, want true")
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("errors.Is(err, ErrNotFound) = true, want false")
		}
	})
	t.Run("NotFound", func(t *testing.T) {
		root, child, _ := mkNestedDirs(t)
		if _, err := Init(child); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}

		cfg, err := Find(root)
		if err == nil {
			t.Fatalf("Find(%q) = %q, want error", root, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Error("errors.Is(err, ErrNotFound) = false, want true")
		}
	})
	t.Run("Permissions", func(t *testing.T) {
		root, child, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}
		if _, err := Init(child); err != nil {
			t.Fatalf("Init(%q) error: %v", child, err)
		}
		path := filepath.Join(child, "devbox.json")
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("os.Chmod(%q, 0o000) error: %v", path, err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o666) })

		cfg, err := Find(child)
		if err == nil {
			t.Fatalf("Find(%q) = %q, want error", child, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Error("errors.Is(err, fs.ErrPermission) = false, want true")
		}
	})
	t.Run("ExactFileBadSyntax", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)

		var (
			path = filepath.Join(root, "devbox.json")
			data = []byte("this isn't json!")
			perm = fs.FileMode(0o666)
		)
		if err := os.WriteFile(path, data, perm); err != nil {
			t.Fatalf("os.WriteFile(%q, []byte(%q), %O) error: %v", path, data, perm, err)
		}

		cfg, err := Find(path)
		if err == nil {
			t.Fatalf("Find(%q) = %q, want error", path, cfg.Root.AbsRootPath)
		}
	})
	t.Run("ExactFilePermissions", func(t *testing.T) {
		root, _, _ := mkNestedDirs(t)
		if _, err := Init(root); err != nil {
			t.Fatalf("Init(%q) error: %v", root, err)
		}
		path := filepath.Join(root, "devbox.json")
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("os.Chmod(%q, 0o000) error: %v", path, err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o666) })

		cfg, err := Find(path)
		if err == nil {
			t.Fatalf("Find(%q) = %q, want error", path, cfg.Root.AbsRootPath)
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Error("errors.Is(err, fs.ErrPermission) = false, want true")
		}
	})
}

// mkNestedDirs sets up a nested directory structure for Find and Open tests.
func mkNestedDirs(t *testing.T) (root, child, nested string) {
	t.Helper()

	root = t.TempDir()
	child = filepath.Join(root, "child")
	nested = filepath.Join(child, "nested")
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	perm := fs.FileMode(0o777)
	if err := os.MkdirAll(nested, perm); err != nil {
		t.Fatalf("os.MkdirAll(%q, %O) error: %v", nested, perm, err)
	}
	return root, child, nested
}

func TestDefault(t *testing.T) {
	path := filepath.Join(t.TempDir())
	cfg := DefaultConfig()
	inBytes := cfg.Root.Bytes()
	if _, err := hujson.Parse(inBytes); err != nil {
		t.Fatalf("default config JSON is invalid: %v\n%s", err, inBytes)
	}
	err := cfg.Root.SaveTo(path)
	if err != nil {
		t.Fatal("got save error:", err)
	}
	out, err := Open(filepath.Join(path, configfile.DefaultName))
	if err != nil {
		t.Fatal("got load error:", err)
	}
	if diff := cmp.Diff(
		cfg,
		out,
		cmpopts.IgnoreUnexported(configfile.ConfigFile{}, configfile.PackagesMutator{}, Config{}),
		cmpopts.IgnoreFields(configfile.ConfigFile{}, "AbsRootPath"),
	); diff != "" {
		t.Errorf("configs not equal (-in +out):\n%s", diff)
	}

	outBytes := out.Root.Bytes()
	if _, err := hujson.Parse(outBytes); err != nil {
		t.Fatalf("loaded default config JSON is invalid: %v\n%s", err, outBytes)
	}
	if string(inBytes) != string(outBytes) {
		t.Errorf("got different JSON after load/save/load:\ninput:\n%s\noutput:\n%s", inBytes, outBytes)
	}
}
