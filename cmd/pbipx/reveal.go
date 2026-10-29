package main

import "github.com/craigalodon/pbipx/internal/secret"

// revealValue is the only call of secret.Reveal in pbipx (principle 2). It is
// reached only when --show-values has passed every check in showValuesRefusal;
// this file alone is excluded from the forbidigo rule in .golangci.yml.
func revealValue(v secret.Value) string {
	return secret.Reveal(v)
}
