// go-coronanet - Coronavirus social distancing network
// Copyright (c) 2020 Péter Szilágyi. All rights reserved.

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/coronanet/go-coronanet/rest"
	"github.com/moby/moby/pkg/reexec"
)

// Hook into the startup process to redirect the self reexec into the live main.
func init() {
	reexec.Register("coronanet", func() {
		main()
		os.Exit(0)
	})
}

// TestMain runs if the binary is executed as a test. If we are re-executing
// ourselves, proceed, otherwise descend into the tests.
func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	os.Exit(m.Run())
}

// testNode is a test runner with some contextual metadata to allow interacting
// with it more easily and cleaning it up.
type testNode struct {
	*rest.API // Embedded API to allow directly calling methods

	tempdir string
	command *exec.Cmd
}

// newTestNode boots up a new coronanet instance and waits until it opens its
// API listener port.
func newTestNode(datadir string, args ...string) (*testNode, error) {
	// Create a temporary folder for all the junk
	tempdir := ""
	if datadir == "" {
		dir, err := ioutil.TempDir("", "")
		if err != nil {
			return nil, err
		}
		tempdir, datadir = dir, dir
	}
	// Create and start the testing process
	cmd := reexec.Command(append([]string{"coronanet", "--datadir", datadir}, args...)...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Wait until the API port is decided
	var apiport int
	for i := 0; i < 30; i++ {
		blob, err := ioutil.ReadFile(filepath.Join(datadir, "apiport"))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if apiport, err = strconv.Atoi(string(blob)); err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
	}
	if apiport == 0 {
		cmd.Process.Kill()
		return nil, errors.New("failed to retrieve API port")
	}
	return &testNode{
		API:     rest.NewAPI(fmt.Sprintf("http://127.0.0.1:%d", apiport)),
		tempdir: tempdir,
		command: cmd,
	}, nil
}

// close cleans up a live testing instance.
func (t *testNode) close() {
	t.command.Process.Signal(syscall.SIGABRT)
	t.command.Wait()

	if t.tempdir != "" {
		os.RemoveAll(t.tempdir)
	}
}
