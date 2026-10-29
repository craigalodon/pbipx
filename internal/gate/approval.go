package gate

import (
	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/policy"
)

// RequireApproval is an Options.CheckPolicy that denies every call unless
// the policy is the one a person approved (ADR-0006): policy-unapproved when
// there is no approval or the policy changed since, internal-error when the
// store cannot be found or read. A nil store is the one in
// approval.ConfigDir, found on each call. Messages name the policy by its
// file name only.
func RequireApproval(store *approval.Store) func(root, policyPath string) *Decision {
	return func(root, policyPath string) *Decision {
		s := store
		if s == nil {
			var err error
			if s, err = approval.Open(); err != nil {
				return denial(ReasonInternalError, "the policy approval store cannot be found; the call is denied")
			}
		}
		st, err := s.Status(root, policyPath)
		if err != nil {
			return denial(ReasonInternalError, "the policy approval cannot be read; the call is denied")
		}
		switch st {
		case approval.Approved:
			return nil
		case approval.Changed:
			return denial(ReasonPolicyUnapproved,
				"%s changed since it was approved; a person reviews it and runs 'pbipx policy approve' at a terminal", policy.FileName)
		case approval.Missing:
			return denial(ReasonPolicyError, "%s does not exist", policy.FileName)
		}
		return denial(ReasonPolicyUnapproved,
			"%s is not approved on this machine; a person reviews it and runs 'pbipx policy approve' at a terminal", policy.FileName)
	}
}

func denial(reason, format string, args ...any) *Decision {
	d := deny(reason, format, args...)
	return &d
}
