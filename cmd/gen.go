////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

// The following directive is necessary to make the package coherent:

// +build ignore

// This program generates cmd/version_vars.go. It can be invoked by running
// go generate
package main

import (
	"bufio"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

func generateGitVersion() string {
	cmd := exec.Command("git", "show", "--oneline")
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(stdoutStderr)))
	for scanner.Scan() {
		return scanner.Text()
	}
	return "UNKNOWNVERSION"
}

func readGlideLock() string {
	r, _ := ioutil.ReadFile("../glide.lock")
	return string(r)
}

func main() {
	gitversion := generateGitVersion()
	glidedependencies := readGlideLock()

	f, err := os.Create("version_vars.go")
	die(err)
	defer f.Close()

	packageTemplate.Execute(f, struct {
		Timestamp time.Time
		GITVER    string
		GLIDEDEPS string
	}{
		Timestamp: time.Now(),
		GITVER:    gitversion,
		GLIDEDEPS: glidedependencies,
	})
}

func die(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

var packageTemplate = template.Must(template.New("").Parse(
	"// Code generated by go generate; DO NOT EDIT.\n" +
		"// This file was generated by robots at\n" +
		"// {{ .Timestamp }}\n" +
		"package cmd\n\n" +
		"const GITVERSION = `{{ .GITVER }}`\n" +
		"const SEMVER = \"0.0.0a\"\n" +
		"const GLIDEDEPS = `{{ .GLIDEDEPS }}`\n"))
