package gate

import (
	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/source"
)

// RequireApproval is an Options.CheckPolicy that denies every call unless
// the policy's bytes are one of the policies a person approved for the
// clone the repository belongs to (ADR-0006):
// policy-unapproved when there is no approval or the policy changed since,
// internal-error when git cannot name the clone, or the store cannot be
// found or read, or is inside the repository or the clone (Store.InClone).
// A nil store is the one in approval.ConfigDir, found on each
// call. Messages name the policy by its file name only.
func RequireApproval(store *approval.Store) func(root string, policyData []byte) *Decision {
	return func(root string, policyData []byte) *Decision {
		s := store
		if s == nil {
			var err error
			if s, err = approval.Open(); err != nil {
				return denial(ReasonInternalError, "the policy approval store cannot be found; the call is denied")
			}
		}
		repo, err := Identify(root)
		if err != nil {
			return denial(ReasonInternalError, "git could not say which clone the repository belongs to; the call is denied")
		}
		if s.InClone(repo) {
			return denial(ReasonInternalError,
				"the policy approval store (%s) is inside the repository, where an agent could write an approval; the call is denied", approval.EnvConfigDir)
		}
		st, err := s.Check(repo, approval.Digest(policyData))
		if err != nil {
			return denial(ReasonInternalError, "the policy approval cannot be read; the call is denied")
		}
		switch st {
		case approval.Approved:
			return nil
		case approval.Changed:
			return denial(ReasonPolicyUnapproved,
				"%s changed since it was approved; a person reviews it and runs 'pbipx policy approve' at a terminal", policy.FileName)
		}
		return denial(ReasonPolicyUnapproved,
			"%s is not approved on this machine; a person reviews it and runs 'pbipx policy approve' at a terminal", policy.FileName)
	}
}

// Identify is what the approval of the work tree at root is recorded for:
// its clone's shared git directory, and the work tree itself.
func Identify(root string) (approval.Repository, error) {
	gitDir, err := (&source.Repo{Root: root}).CommonDir()
	if err != nil {
		return approval.Repository{}, err
	}
	return approval.Repository{GitDir: gitDir, Root: root}, nil
}

func denial(reason, format string, args ...any) *Decision {
	d := deny(reason, format, args...)
	return &d
}
