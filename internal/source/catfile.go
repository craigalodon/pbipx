package source

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// catFile reads objects through one long-running `git cat-file --batch`, so
// a scan of the index or a commit starts git once, not once per file.
type catFile struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func startCatFile(dir string) (*catFile, error) {
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = dir
	cmd.Stderr = nil
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, &GitError{Op: "cat-file", Exit: -1}
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &GitError{Op: "cat-file", Exit: -1}
	}
	if err := cmd.Start(); err != nil {
		return nil, &GitError{Op: "cat-file", Exit: -1}
	}
	return &catFile{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

// errNotBlob is an object that is missing or not a file's content, such as
// a submodule's commit.
var errNotBlob = errors.New("the object is not a blob")

// read returns one blob's content.
func (c *catFile) read(oid string) ([]byte, error) {
	if _, err := fmt.Fprintln(c.in, oid); err != nil {
		return nil, &GitError{Op: "cat-file", Exit: -1}
	}
	header, err := c.out.ReadString('\n')
	if err != nil {
		return nil, &GitError{Op: "cat-file", Exit: -1}
	}
	// "<oid> <type> <size>", or "<name> missing".
	fields := strings.Fields(header)
	if len(fields) != 3 {
		return nil, errNotBlob
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 {
		return nil, &GitError{Op: "cat-file", Exit: 0}
	}
	data := make([]byte, size+1) // the content and a trailing newline
	if _, err := io.ReadFull(c.out, data); err != nil {
		return nil, &GitError{Op: "cat-file", Exit: -1}
	}
	if fields[1] != "blob" {
		return nil, errNotBlob
	}
	return data[:size], nil
}

func (c *catFile) close() error {
	_ = c.in.Close()
	if err := c.cmd.Wait(); err != nil {
		return gitError("cat-file", err)
	}
	return nil
}
