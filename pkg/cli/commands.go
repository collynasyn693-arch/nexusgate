package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// Command represents an individual CLI action.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Run         func(ctx context.Context, args []string) error
}

// App represents the top-level CLI command tree.
type App struct {
	commands map[string]*Command
	out      io.Writer
	errOut   io.Writer
}

// NewApp creates a new NexusGate CLI command processor.
func NewApp() *App {
	app := &App{
		commands: make(map[string]*Command),
		out:      os.Stdout,
		errOut:   os.Stderr,
	}
	app.registerBuiltins()
	return app
}

// SetOutput configures stdout/stderr writers for testing.
func (a *App) SetOutput(out, errOut io.Writer) {
	a.out = out
	a.errOut = errOut
}

// Register adds a command to the app.
func (a *App) Register(cmd *Command) {
	a.commands[cmd.Name] = cmd
	for _, alias := range cmd.Aliases {
		a.commands[alias] = cmd
	}
}

// registerBuiltins sets up default help and version commands.
func (a *App) registerBuiltins() {
	a.Register(&Command{
		Name:        "help",
		Aliases:     []string{"-h", "--help"},
		Description: "Display help information for NexusGate commands",
		Usage:       "nexusgate help [command]",
		Run: func(ctx context.Context, args []string) error {
			if len(args) > 0 {
				cmdName := args[0]
				if cmd, ok := a.commands[cmdName]; ok {
					fmt.Fprintf(a.out, "Usage: %s\n\n%s\n", cmd.Usage, cmd.Description)
					return nil
				}
				return fmt.Errorf("unknown command: %s", cmdName)
			}
			a.PrintHelp()
			return nil
		},
	})
}

// PrintHelp outputs the summary of available commands.
func (a *App) PrintHelp() {
	fmt.Fprintf(a.out, "NexusGate - Zero-Daemon High-Performance Edge Gateway for Termux ARM64\n\n")
	fmt.Fprintf(a.out, "Usage:\n  nexusgate <command> [arguments] [flags]\n\n")
	fmt.Fprintf(a.out, "Available Commands:\n")

	printed := make(map[string]bool)
	for _, name := range []string{"start", "run", "attach", "validate", "reload", "drain", "version", "help"} {
		if cmd, ok := a.commands[name]; ok && !printed[cmd.Name] {
			fmt.Fprintf(a.out, "  %-12s %s\n", cmd.Name, cmd.Description)
			printed[cmd.Name] = true
		}
	}
	fmt.Fprintf(a.out, "\nUse 'nexusgate help <command>' for more information about a command.\n")
}

// Execute parses the CLI arguments and dispatches to the requested command.
func (a *App) Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		a.PrintHelp()
		return nil
	}

	subcmd := args[0]
	if subcmd == "-h" || subcmd == "--help" {
		a.PrintHelp()
		return nil
	}

	cmd, ok := a.commands[subcmd]
	if !ok {
		return fmt.Errorf("unknown command %q. Run 'nexusgate help' for usage", subcmd)
	}

	return cmd.Run(ctx, args[1:])
}

// ParseFlags is a helper to parse simple key-value flags (-c <path>, --config <path>, etc.).
func ParseFlags(args []string) (map[string]string, []string) {
	flags := make(map[string]string)
	var posArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			k := arg[2:]
			if eqIdx := strings.IndexByte(k, '='); eqIdx != -1 {
				flags[k[:eqIdx]] = k[eqIdx+1:]
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags[k] = args[i+1]
				i++
			} else {
				flags[k] = "true"
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			k := arg[1:]
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags[k] = args[i+1]
				i++
			} else {
				flags[k] = "true"
			}
		} else {
			posArgs = append(posArgs, arg)
		}
	}

	return flags, posArgs
}

// RegisterAllCommands registers all subcommands on the CLI application.
func RegisterAllCommands(a *App) {
	RegisterValidateCommand(a)
	RegisterStartCommand(a)
}

