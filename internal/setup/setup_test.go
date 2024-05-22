package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

type testTask struct {
	RunFunc      func(ctx context.Context) error
	NeedsRunFunc func(ctx context.Context, lastRun RunInfo) bool
}

func (t *testTask) Run(ctx context.Context) error {
	return t.RunFunc(ctx)
}

func (t *testTask) NeedsRun(ctx context.Context, lastRun RunInfo) bool {
	return t.NeedsRunFunc(ctx, lastRun)
}

func TestTaskNeedsRunTrue(t *testing.T) {
	tempXDGStateDir(t)

	ran := false
	task := &testTask{
		RunFunc: func(ctx context.Context) error {
			ran = true
			return nil
		},
		NeedsRunFunc: func(context.Context, RunInfo) bool {
			return true
		},
	}

	err := Run(context.Background(), t.Name(), task)
	if err != nil {
		t.Error("got non-nil error:", err)
	}
	if !ran {
		t.Error("got ran = false, want true")
	}
}

func TestTaskNeedsRunFalse(t *testing.T) {
	tempXDGStateDir(t)

	ran := false
	task := &testTask{
		RunFunc: func(ctx context.Context) error {
			ran = true
			return nil
		},
		NeedsRunFunc: func(context.Context, RunInfo) bool {
			return false
		},
	}

	err := Run(context.Background(), t.Name(), task)
	if err != nil {
		t.Error("got non-nil error:", err)
	}
	if ran {
		t.Error("got ran = true, want false")
	}
}

func TestTaskLastRun(t *testing.T) {
	tempXDGStateDir(t)

	task := &testTask{
		RunFunc:      func(ctx context.Context) error { return nil },
		NeedsRunFunc: func(context.Context, RunInfo) bool { return true },
	}
	err := Run(context.Background(), t.Name(), task)
	if err != nil {
		t.Error("got non-nil error on first run:", err)
	}

	task.NeedsRunFunc = func(ctx context.Context, lastRun RunInfo) bool {
		if lastRun.Time.IsZero() {
			t.Error("got zero lastRun.Time on second run")
		}
		if lastRun.Version == "" {
			t.Error("got empty lastRun.Version on second run")
		}
		if lastRun.Error != "" {
			t.Errorf("got non-empty lastRun.Error on second run: %v", lastRun.Error)
		}
		return false
	}
	err = Run(context.Background(), t.Name(), task)
	if err != nil {
		t.Error("got non-nil error on second run:", err)
	}
}

func TestTaskConfirmPromptAllow(t *testing.T) {
	tempXDGStateDir(t)

	task := &testTask{
		RunFunc:      func(ctx context.Context) error { return nil },
		NeedsRunFunc: func(context.Context, RunInfo) bool { return true },
	}

	setPromptResponse(t, true)
	err := ConfirmRun(context.Background(), t.Name(), task, "continue?")
	if err != nil {
		t.Error("got non-nil error:", err)
	}
}

func TestTaskConfirmPromptDeny(t *testing.T) {
	tempXDGStateDir(t)

	task := &testTask{
		RunFunc:      func(ctx context.Context) error { return nil },
		NeedsRunFunc: func(context.Context, RunInfo) bool { return true },
	}

	setPromptResponse(t, false)
	err := ConfirmRun(context.Background(), t.Name(), task, "continue?")
	if err == nil {
		t.Error("got nil error, want ErrUserRefused")
	} else if !errors.Is(err, ErrUserRefused) {
		t.Error("got errors.Is(err, ErrUserRefused) == false for error:", err)
	}
}

// TestSudoDevbox uses sudo on the current test binary to recursively call
// itself as root. This test can only be run manually (because it needs sudo)
// but is still useful for testing after making any changes to the sudo code.
//
//   - Within the test we check if os.Getuid() == 0 to act differently depending
//     on if we're the sudo test process or the parent (non-sudo) test process.
//   - The sudo version of the test creates a "test-sudo-devbox-result" file.
//   - The non-sudo version of the test looks for the same file to know if the
//     sudo worked.
func TestSudoDevbox(t *testing.T) {
	t.Skip("this test must be run manually because it requires sudo")

	ctx := context.Background()
	key := "test-sudo-devbox"
	resultFile := key + "-result"

	// Non-sudo process cleans up the result file.
	os.Remove(resultFile)
	t.Cleanup(func() {
		if os.Getuid() != 0 {
			os.Remove(resultFile)
		}
	})

	task := &testTask{}
	task.RunFunc = func(ctx context.Context) error {
		ran, err := SudoDevbox(ctx, "-test.run", "^"+t.Name()+"$")
		if ran || err != nil {
			return err
		}

		// Create a result file to indicate to the non-sudo process that
		// we ran as root successfully.
		if os.Getuid() == 0 {
			return os.WriteFile(resultFile, nil, 0o666)
		}
		err = fmt.Errorf("task.NeedsRun not running as root after calling SudoDevbox")
		t.Error(err)
		return err
	}
	task.NeedsRunFunc = func(ctx context.Context, lastRun RunInfo) bool {
		if os.Getuid() == 0 {
			t.Error("task.NeedsRun called in sudo process, but should only be called in user process")
		}
		return true
	}

	old := defaultPrompt
	t.Cleanup(func() { defaultPrompt = old })
	defaultPrompt = func(msg string) (response any, err error) {
		if os.Getuid() == 0 {
			err = fmt.Errorf("user prompted again while already running as sudo")
			t.Error(err)
			return false, err
		}
		return true, nil
	}

	err := ConfirmRun(ctx, key, task, "Allow sudo to run Devbox as root?")
	if err != nil {
		t.Error("got ConfirmRun error:", err)
	}
	if _, err := os.Stat(resultFile); err != nil {
		t.Error("got missing sudo result file:", err)
	}
}

func tempXDGStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func setPromptResponse(t *testing.T, a any) {
	old := defaultPrompt
	t.Cleanup(func() { defaultPrompt = old })
	defaultPrompt = func(string) (any, error) { return a, nil }
}
