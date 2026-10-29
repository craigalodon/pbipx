package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/source"
)

// now is the clock baseline expiry is judged by. A test replaces it.
var now = time.Now

// openRepo finds the repository the working directory is in. A failure is
// reported on cmd and returned as the error to end the command with.
func openRepo(cmd *cobra.Command, format string) (string, *source.Repo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, operational(cmd, "the working directory cannot be read")
	}
	repo, err := source.Open(cwd)
	if err != nil {
		return cwd, nil, repoError(cmd, format, err)
	}
	return cwd, repo, nil
}

// repoError reports an error from source.Open.
func repoError(cmd *cobra.Command, format string, err error) error {
	if errors.Is(err, source.ErrNotARepository) {
		return failure(cmd, format, finding.NotARepository, "the working directory is not in a git repository")
	}
	return operational(cmd, gitDetail(err))
}

// loadPolicy loads the policy: flagPath, relative to cwd, when given, or
// .pbipx.toml at the repository root. A *policy.Error is the policy-error
// failure.
func loadPolicy(cmd *cobra.Command, format, cwd string, repo *source.Repo, flagPath string) (*policy.Policy, error) {
	p, _, err := loadPolicyData(cmd, format, cwd, repo, flagPath)
	return p, err
}

// loadPolicyData is loadPolicy, and also returns the bytes the policy was
// parsed from.
func loadPolicyData(cmd *cobra.Command, format, cwd string, repo *source.Repo, flagPath string) (*policy.Policy, []byte, error) {
	path, name := "", policy.FileName
	switch {
	case flagPath != "":
		path, name = resolve(cwd, flagPath), "the --policy file"
	case repo != nil:
		path = filepath.Join(repo.Root, policy.FileName)
	default:
		return nil, nil, failure(cmd, format, finding.NotARepository, "the working directory is not in a git repository, and no --policy was given")
	}
	p, data, err := policy.LoadData(path)
	if err != nil {
		var pe *policy.Error
		if errors.As(err, &pe) {
			return nil, nil, failure(cmd, format, finding.PolicyError, describe(name, pe.Line, pe.Key, pe.Msg))
		}
		return nil, nil, failure(cmd, format, finding.PolicyError, name+": cannot be loaded")
	}
	return p, data, nil
}

// baselinePath is flagPath, relative to cwd, when given, or the default at
// the repository root, with the name to report it by.
func baselinePath(cwd string, repo *source.Repo, flagPath string) (path, name string) {
	if flagPath != "" {
		return resolve(cwd, flagPath), "the --baseline file"
	}
	return filepath.Join(repo.Root, baseline.FileName), baseline.FileName
}

// loadBaseline loads the baseline at path. A *baseline.Error is the
// baseline-error failure.
func loadBaseline(cmd *cobra.Command, format, path, name string) (*baseline.Baseline, error) {
	b, err := baseline.Load(path)
	if err != nil {
		var be *baseline.Error
		if errors.As(err, &be) {
			key := be.Key
			if be.Entry != "" {
				key = strings.TrimSpace("entry " + be.Entry + " " + key)
			}
			return nil, failure(cmd, format, finding.BaselineError, describe(name, be.Line, key, be.Msg))
		}
		return nil, failure(cmd, format, finding.BaselineError, name+": cannot be loaded")
	}
	return b, nil
}

// describe is a policy or baseline error as structure: the file by the name
// pbipx knows it by (never a path typed on the command line), a line, a key
// and what is wrong.
func describe(name string, line int, key, msg string) string {
	var b strings.Builder
	b.WriteString(name)
	if line > 0 {
		fmt.Fprintf(&b, ":%d", line)
	}
	b.WriteString(": ")
	if key != "" {
		b.WriteString(key + " ")
	}
	b.WriteString(msg)
	return b.String()
}

// gitDetail describes a source error by structure only.
func gitDetail(err error) string {
	var ge *source.GitError
	if errors.As(err, &ge) {
		return ge.Error()
	}
	return "reading the files to scan failed"
}

func resolve(cwd, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}
