package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVersionDoesNotNeedConfig(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-c", "/does-not-exist", "--version"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "commit") {
		t.Fatal(out.String())
	}
}
func TestSyntaxCheckDoesNotExecuteScript(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "effect")
	script := filepath.Join(dir, "main.lua")
	if err := os.WriteFile(script, []byte("local f=io.open("+strconv.Quote(marker)+",'w'); f:write('bad'); f:close()"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkLuaSyntax(script); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("script ran during syntax check")
	}
}
