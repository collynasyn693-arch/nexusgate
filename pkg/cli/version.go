package cli

import (
	"context"
	"fmt"
	"runtime"
)

var (
	// Version is the current semantic release version of NexusGate.
	Version = "1.0.0"
	// GitCommit is the specific git commit SHA, populated at link time.
	GitCommit = "dev"
	// BuildDate is the compilation timestamp, populated at link time.
	BuildDate = "unknown"
	// Target specifies the deployment target architecture.
	Target = "termux/arm64"
)

// RegisterVersionCommand registers the 'version' subcommand on the application.
func RegisterVersionCommand(a *App) {
	a.Register(&Command{
		Name:        "version",
		Aliases:     []string{"-v", "--version"},
		Description: "Print NexusGate version, git commit, build date, and target platform",
		Usage:       "nexusgate version [--json]",
		Run: func(ctx context.Context, args []string) error {
			flags, _ := ParseFlags(args)
			if flags["json"] == "true" {
				fmt.Fprintf(a.out, `{"version":%q,"git_commit":%q,"build_date":%q,"go_version":%q,"platform":%q,"target":%q}`+"\n",
					Version, GitCommit, BuildDate, runtime.Version(), fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH), Target)
				return nil
			}

			fmt.Fprintf(a.out, "NexusGate v%s\n", Version)
			fmt.Fprintf(a.out, "  Git Commit: %s\n", GitCommit)
			fmt.Fprintf(a.out, "  Build Date: %s\n", BuildDate)
			fmt.Fprintf(a.out, "  Go Version: %s\n", runtime.Version())
			fmt.Fprintf(a.out, "  Platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(a.out, "  Target:     %s\n", Target)
			return nil
		},
	})
}
