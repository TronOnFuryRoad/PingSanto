package upgrade

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"syscall"
)

// ErrRestartDeferred is returned when a restarter chooses to delay restart to an external system.
var ErrRestartDeferred = errors.New("restart deferred")

// Restarter restarts the agent process using the installed binary.
type Restarter interface {
	Restart(ctx context.Context, binaryPath string, args []string, env []string) error
}

// ExecRestarter replaces the current process using syscall.Exec.
type ExecRestarter struct {
	Logger *log.Logger
}

// Restart invokes execve on the provided binary path.
func (r *ExecRestarter) Restart(ctx context.Context, binaryPath string, args []string, env []string) error {
	if strings.TrimSpace(binaryPath) == "" {
		return errors.New("binary path required for restart")
	}
	if args == nil || len(args) == 0 {
		args = []string{binaryPath}
	}
	if env == nil {
		env = os.Environ()
	}
	if r.Logger != nil {
		r.Logger.Printf("upgrade restarter: exec %s", binaryPath)
	}
	return syscall.Exec(binaryPath, args, env)
}
