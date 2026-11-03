package gate_test

// Payloads that feed a name-producing command into a runner that hands
// the names to a reader: the placeholder of xargs -I, appended input, GNU
// parallel's placeholders (when parallel is installed), and find -exec's
// {}, directly or through a nested shell line. The names are the real
// ones git or find list, so a stub reader is given the protected file's
// path as bash really runs the line.
func init() {
	producers := []string{
		"git ls-files",
		"git diff --name-only HEAD~1",
		"git ls-files pbip",
		"find . -type f",
		"printf '%s\\n' " + leakVisual,
	}
	readers := []string{"cat", "head -1", "jq ."}
	var out []string
	for _, p := range producers {
		for _, r := range readers {
			out = append(out,
				p+" | xargs -I{} sh -c '"+r+" {}'",
				p+" | xargs -n1 -I@ bash -c '"+r+" @'",
				p+" | xargs -I{} "+r+" {}",
				p+" | xargs "+r,
				p+" | parallel '"+r+" {}'",
				p+" | parallel "+r,
				p+" | parallel "+r+" {/}",
				p+" | xargs -I{} sh -c '"+r+" < {}'",
				p+" | parallel '"+r+" < {}'",
				p+" | xargs -I{} sh -c '< {} "+r+"'",
			)
		}
	}
	for _, p := range producers {
		out = append(out,
			p+" | xargs -I{} sh -c 'while IFS= read -r l; do cat; done < {}'",
			p+" | xargs -I{} sh -c 'exec 3< {}; cat <&3'",
		)
	}
	for _, r := range readers {
		out = append(out,
			"find . -exec sh -c '"+r+" {}' \\;",
			"find . -type f -exec "+r+" {} +",
			"find pbip -exec sh -c '"+r+" {}' \\;",
		)
	}
	differentialPayloads = append(differentialPayloads, out...)
}
