package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate/claudecode"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/setup"
)

// executable is the running binary's path, which the hook runs. A test
// replaces it.
var executable = os.Executable

type initFlags struct {
	managed   bool
	force     bool
	dryRun    bool
	gitignore bool
}

func newInitCmd() *cobra.Command {
	var f initFlags
	cmd := &cobra.Command{
		Use:   "init [--managed] [--force] [--dry-run] [--gitignore]",
		Short: "write a starting policy and register the gate's agent hook",
		Long: `Set up pbipx in the git repository the working directory is in.

1. Find the directories that hold PBIP folders (*.Report, *.SemanticModel) and
   .pbip files, and write a commented starting policy, .pbipx.toml, that
   protects them. An existing policy is kept unless --force is given. Only
   directory and file names are read, never a file's content.
2. Register the gate as Claude Code's PreToolUse hook for Read, Grep, Bash and
   the file-writing tools, in the user-level settings
   ($CLAUDE_CONFIG_DIR/settings.json, or ~/.claude/settings.json), outside the
   repository, where a project cannot remove it. A project can still turn
   every user-level hook off with disableAllHooks; the gate denies an agent's
   edit of the project's settings and pbipx scan reports a committed one, but
   on shared machines and for teams use --managed. Every other setting and
   hook is kept, and a backup of the file is written beside it. A hook this
   binary already registered is upgraded in place (its command, matcher,
   shell and timeout), and duplicates of it are removed, so re-running init
   after upgrading pbipx is how the hook is kept current. The hook's timeout
   is set to 300 seconds: a hook that times out lets the call proceed, so a
   slow scan must not reach Claude Code's default. A gate hook for another pbipx
   binary is left with a warning, or replaced with --force. The hook runs
   this binary by its absolute path, so init refuses to run from a binary
   inside the repository.
3. Print the next steps.

--managed writes no settings and prints the hook as a drop-in file of Claude
Code's managed settings (managed-settings.d/pbipx.json), for an administrator
to add, instead. --dry-run writes nothing and prints
what init would do. --gitignore appends the .gitignore lines a PBIP repository
needs (.pbi/, *.pbix, *.abf) when they are missing; without it, init lists
them.

init never writes through a symbolic link in the repository: a .pbipx.toml
it would write, or a .gitignore it would append to, that is a link or not a
regular file is a usage error, and nothing is written. The settings file is
refused when, symlinks resolved, it is inside the repository. Each backup of
the settings gets a name of its own, so no run replaces an earlier one.

Exit codes: 0 done, 3 a failure, 4 a usage error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, &f)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.managed, "managed", false, "print the hook for managed settings instead of writing user settings")
	fl.BoolVar(&f.force, "force", false, "overwrite an existing .pbipx.toml, and replace gate hooks for another pbipx binary")
	fl.BoolVar(&f.dryRun, "dry-run", false, "write nothing; print what would be done")
	fl.BoolVar(&f.gitignore, "gitignore", false, "append missing .gitignore lines for Desktop's local state and binaries")
	return cmd
}

func runInit(cmd *cobra.Command, f *initFlags) error {
	out := cmd.OutOrStdout()
	_, repo, err := openRepo(cmd, formatHuman)
	if err != nil {
		return err
	}

	// The gate must not run from the working tree (threat model, layer 4).
	exe, err := gateBinary(repo.Root)
	if err != nil {
		return err
	}
	hook := setup.Hook{
		Command: claudecode.HookCommand(exe, runtime.GOOS),
		Shell:   claudecode.HookShell(runtime.GOOS),
		Timeout: claudecode.HookTimeout,
	}

	// Every check comes before the first write.
	var settingsPath string
	var merge setup.Merge
	if !f.managed {
		if settingsPath, err = setup.SettingsPath(); err != nil {
			return operational(cmd, "the user's home directory cannot be found")
		}
		if settingsInRepository(repo.Root, settingsPath) {
			return usageError{"the Claude Code settings file is inside the repository, where the project could change it; set CLAUDE_CONFIG_DIR or HOME outside it, or use --managed"}
		}
		existing, rerr := os.ReadFile(settingsPath) //nolint:gosec // G304: the user's own Claude Code settings
		if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return operational(cmd, "the Claude Code settings file cannot be read")
		}
		if merge, err = setup.MergeHook(existing, hook, f.force); err != nil {
			return operational(cmd, "the Claude Code settings file is not a JSON object with hooks pbipx can merge into; add the hook by hand (pbipx init --managed prints it)")
		}
	}

	layout, err := setup.Detect(repo.Root)
	if err != nil {
		return operational(cmd, "the repository's directories cannot be listed")
	}
	policyPath := filepath.Join(repo.Root, policy.FileName)
	_, statErr := os.Lstat(policyPath)
	policyExists := statErr == nil
	writePolicy := !policyExists || f.force
	missing, err := setup.MissingIgnores(repo.Root)
	if err != nil {
		return operational(cmd, ".gitignore cannot be read")
	}
	// A file init writes in the repository must be a regular file or
	// absent: through a symbolic link, a write would land outside it.
	if writePolicy && setup.RegularOrAbsent(policyPath) != nil {
		return usageError{policy.FileName + " is a symbolic link or not a regular file; init will not write through it. Replace it with a regular file, or remove it"}
	}
	if f.gitignore && len(missing) > 0 && setup.RegularOrAbsent(filepath.Join(repo.Root, ".gitignore")) != nil {
		return usageError{".gitignore is a symbolic link or not a regular file; init will not write through it. Add the lines by hand"}
	}

	would := ""
	if f.dryRun {
		fmt.Fprintln(out, "Dry run: nothing is written.")
		would = "Would write"
	}

	// 1. The policy.
	switch {
	case !writePolicy:
		fmt.Fprintf(out, "%s exists; left unchanged (--force overwrites it).\n", policy.FileName)
	default:
		if !f.dryRun {
			if err := writePolicyFile(policyPath, setup.Policy(layout), f.force); err != nil {
				return operational(cmd, policy.FileName+" cannot be written")
			}
			if _, err := policy.Load(policyPath); err != nil {
				return operational(cmd, "the written "+policy.FileName+" does not load; this is a bug in pbipx init")
			}
		}
		verb := "Wrote"
		if would != "" {
			verb = would
		}
		fmt.Fprintf(out, "%s %s.\n", verb, policy.FileName)
		describeLayout(out, layout)
	}

	// 2. The hook.
	switch {
	case f.managed:
		snippet, serr := setup.Snippet(hook)
		if serr != nil {
			return operational(cmd, "the hook cannot be rendered")
		}
		fmt.Fprintf(out, "\nNo settings written (--managed). An administrator adds the hook as a drop-in file of\n"+
			"Claude Code's managed settings, which merges every *.json there and needs no other\n"+
			"file edited:\n\n  %s\n\nholding:\n\n%s\n", managedDropInPath(), snippet)
		fmt.Fprintf(out, "# A repository's .claude/settings.json can set \"disableAllHooks\": true, which\n"+
			"# turns off user-level hooks. To stop any repository or user setting from\n"+
			"# turning hooks off, also set this in managed settings (managed-settings.json\n"+
			"# or another drop-in in %s). Only managed hooks then\n"+
			"# run: user and project hooks stop.\n#\n"+
			"#   \"allowManagedHooksOnly\": true\n", managedSettingsDir())
	case merge.Present:
		fmt.Fprintf(out, "The gate's hook is already registered in %s.\n", settingsPath)
	case f.dryRun:
		verb := "register"
		if merge.Updated {
			verb = "update"
		}
		fmt.Fprintf(out, "Would %s the gate's hook (PreToolUse: %s) in %s, keeping a backup.\n", verb, setup.HookMatcher, settingsPath)
	default:
		backup, werr := setup.WriteSettings(settingsPath, merge.Data, now())
		if werr != nil {
			return operational(cmd, "the Claude Code settings file cannot be written")
		}
		verb := "Registered"
		if merge.Updated {
			verb = "Updated"
		}
		fmt.Fprintf(out, "%s the gate's hook (PreToolUse: %s) in %s.\n", verb, setup.HookMatcher, settingsPath)
		if backup != "" {
			fmt.Fprintf(out, "The previous settings are kept in %s.\n", backup)
		}
	}
	if !f.managed {
		fmt.Fprintln(out, "A repository can turn off user-level hooks (disableAllHooks); on shared machines and for teams, use pbipx init --managed.")
	}
	if merge.Others > 0 {
		fmt.Fprintf(out, "Warning: %s registers %s that runs another pbipx binary. A hook whose binary is gone denies every call; remove it if it is stale, or re-run with --force to replace it.\n",
			settingsPath, plural(merge.Others, "a gate hook", "gate hooks"))
	}

	// 3. .gitignore.
	gitignored := false
	if f.gitignore && len(missing) > 0 {
		if !f.dryRun {
			if err := setup.AppendIgnores(repo.Root, missing); err != nil {
				return operational(cmd, ".gitignore cannot be written")
			}
		}
		verb := "Added"
		if f.dryRun {
			verb = "Would add"
		}
		fmt.Fprintf(out, "%s to .gitignore: %s\n", verb, strings.Join(missing, " "))
		gitignored = true
	}

	nextSteps(out, missing, gitignored)
	return nil
}

// gateBinary is the absolute path of the running binary, refused when it is
// inside the repository at root or is a temporary build that will be gone.
func gateBinary(root string) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", usageError{"the running binary's path cannot be found"}
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return "", usageError{"the running binary's path cannot be found"}
	}
	resolved := exe
	if r, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		resolved = r
	}
	if within(root, exe) || within(root, resolved) {
		return "", usageError{"pbipx is running from inside the repository, where an agent could change it; install it outside (README, Install) and run init with that binary"}
	}
	for seg := range strings.SplitSeq(filepath.ToSlash(resolved), "/") {
		if strings.HasPrefix(seg, "go-build") {
			return "", usageError{"pbipx is running from a temporary build (go run); install it and run init with the installed binary"}
		}
	}
	return exe, nil
}

// settingsInRepository reports whether the settings file at path is inside
// the repository at root, as written or with symlinks resolved (of the file,
// or of its deepest existing ancestor when it is not there yet), against
// the root as given and resolved.
func settingsInRepository(root, path string) bool {
	roots := []string{root, approval.ResolveExisting(root)}
	for _, p := range []string{path, approval.ResolveExisting(path)} {
		for _, r := range roots {
			if within(r, p) {
				return true
			}
		}
	}
	return false
}

// within reports whether path is dir or below it.
func within(dir, path string) bool {
	if runtime.GOOS == "windows" {
		dir, path = strings.ToLower(dir), strings.ToLower(path)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// writePolicyFile writes the policy, refusing to replace a file unless
// force is set, and never through a symbolic link where the platform has
// O_NOFOLLOW (the caller has checked with Lstat in any case).
func writePolicyFile(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | setup.NoFollow
	}
	f, err := os.OpenFile(path, flags, 0o644) //nolint:gosec // G302,G304: a repository file every collaborator reads
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// describeLayout prints the protected roots the policy names. Directory
// names are schema, not values.
func describeLayout(w io.Writer, l setup.Layout) {
	if len(l.Roots) == 0 {
		fmt.Fprintf(w, "No PBIP project was found, so the policy protects a placeholder root, %q. Edit protected_roots and the globs before relying on it.\n", setup.PlaceholderRoot)
	} else {
		fmt.Fprintln(w, "Protected roots:")
		for _, r := range l.Roots {
			fmt.Fprintf(w, "  %s\n", r)
		}
	}
	if l.TopLevelPBIP {
		fmt.Fprintln(w, "A .pbip file at the repository root is scanned as a report.")
	}
	if l.Skipped > 0 {
		fmt.Fprintf(w, "Skipped %s whose name cannot be written into the policy (glob, quote or control characters); add %s by hand.\n",
			plural(l.Skipped, "a project directory", fmt.Sprintf("%d project directories", l.Skipped)), plural(l.Skipped, "it", "them"))
	}
}

// managedSettingsDir is where Claude Code reads managed settings on this
// system.
func managedSettingsDir() string { return setup.ManagedSettingsDir(runtime.GOOS) }

// managedDropInPath is the drop-in file init --managed asks an administrator
// to add: Claude Code merges every *.json in managed-settings.d, beside
// managed-settings.json, in alphabetical order.
func managedDropInPath() string {
	if runtime.GOOS == "windows" {
		return managedSettingsDir() + `\managed-settings.d\pbipx.json`
	}
	return path.Join(managedSettingsDir(), "managed-settings.d", "pbipx.json")
}

// nextSteps prints what the adopter does after init.
func nextSteps(w io.Writer, missing []string, gitignored bool) {
	rev := "<a release tag>"
	if version != "dev" {
		rev = version
	}
	steps := []string{
		"Review " + policy.FileName + ", above all [columns]: the column lists are a generic start. Add your\n" +
			"   models' identifying columns, and remove what does not apply.",
		"Approve the policy at a terminal: pbipx policy approve (ADR-0006). The gate denies\n" +
			"   every call while the policy is unapproved, and again after each change until it\n" +
			"   is re-approved.",
		"Add the commit hooks to .pre-commit-config.yaml (the pbipx binary must be on PATH):\n\n" +
			"     - repo: https://github.com/craigalodon/pbipx\n" +
			"       rev: " + rev + "\n" +
			"       hooks:\n" +
			"         - id: pbipx-scan\n" +
			"         - id: pbipx-commit-msg\n\n" +
			"   then run: pre-commit install --hook-type pre-commit --hook-type commit-msg",
	}
	if len(missing) > 0 && !gitignored {
		steps = append(steps, "Add to .gitignore: "+strings.Join(missing, " ")+" (pbipx init --gitignore does it).")
	}
	steps = append(steps, "Commit "+policy.FileName+", and start a new Claude Code session: a running one keeps\n"+
		"   the hooks it started with.")
	fmt.Fprintln(w, "\nNext steps:")
	for i, s := range steps {
		fmt.Fprintf(w, "%d. %s\n", i+1, s)
	}
}
