package main

//go:generate go run ./gh-action-integration-generator.go

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// testsToSplit defines tests that should be split into multiple CI jobs.
// Key is the test function name, value is a list of subtest prefixes.
// Each prefix becomes a separate CI job as "TestName$/^prefix".
//
// Wall clock across the matrix is bounded by the longest single job, not by
// the total, so the tests worth splitting are the slowest ones. Measured
// against a full run: [TestAutoApproveMultiNetwork] took 13-18 minutes per
// approver while every other job averaged four, because CI split it by
// approver (4 subtests each) rather than by subtest. Splitting to the leaf
// costs nothing in coverage: the same subtests run, in more jobs.
//
// A prefix must name a real subtest. [TestAutoApproveMultiNetwork] composes
// its names as "<approver>-advertiseduringup-<bool>-pol-<mode>", the auth-key
// relogin tests as "with-https-<bool>", and the rest take theirs from a table.
var testsToSplit = map[string][]string{
	"TestAutoApproveMultiNetwork": autoApproveSubtests(),
	"TestAuthKeyLogoutAndReloginSameUser": {
		"with-https-true",
		"with-https-false",
	},
	"TestAuthKeyLogoutAndReloginSameUserExpiredKey": {
		"with-https-true",
		"with-https-false",
	},
	"TestSSHLocalpart": {
		"MemberAndTagged",
		"AutogroupSelf",
		"LocalpartPlusRoot",
	},
	"TestOIDC024UserCreation": {
		"no-migration-verified-email",
		"no-migration-not-verified-email",
		"migration-no-strip-domains-not-verified-email",
	},
}

// autoApproveSubtests enumerates the leaf subtests [TestAutoApproveMultiNetwork]
// generates: one per approver, policy mode and advertise-during-up combination.
func autoApproveSubtests() []string {
	approvers := []string{
		"authkey-tag", "authkey-user", "authkey-group",
		"webauth-tag", "webauth-user", "webauth-group",
	}

	var out []string

	for _, approver := range approvers {
		for _, advertiseDuringUp := range []string{"false", "true"} {
			for _, polMode := range []string{"database", "file"} {
				out = append(out, fmt.Sprintf(
					"%s-advertiseduringup-%s-pol-%s",
					approver, advertiseDuringUp, polMode,
				))
			}
		}
	}

	return out
}

// expandTests takes a list of test names and expands any that need splitting
// into multiple subtest patterns.
func expandTests(tests []string) []string {
	var expanded []string

	for _, test := range tests {
		prefixes, ok := testsToSplit[test]
		if !ok {
			expanded = append(expanded, test)

			continue
		}

		// The runner wraps the pattern in ^...$ and go test splits it on "/",
		// matching each part unanchored. Anchor both ends of the test name
		// ourselves, or "^TestAuthKeyLogoutAndReloginSameUser" also selects
		// TestAuthKeyLogoutAndReloginSameUserExpiredKey and runs it twice.
		// ".*" on the prefix lets it match the rest of the subtest name.
		for _, prefix := range prefixes {
			expanded = append(expanded, fmt.Sprintf("%s$/^%s.*", test, prefix))
		}
	}

	return expanded
}

func findTests() []string {
	rgBin, err := exec.LookPath("rg")
	if err != nil {
		log.Fatalf("failed to find rg (ripgrep) binary")
	}

	args := []string{
		"--type", "go",
		"--regexp", "func (Test.+)\\(.*",
		"--max-depth", "1",
		"../../integration/",
		"--replace", "$1",
		"--sort", "path",
		"--no-line-number",
		"--no-filename",
		"--no-heading",
	}

	cmd := exec.Command(rgBin, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		log.Fatalf("failed to run command: %s", err)
	}

	tests := strings.Split(strings.TrimSpace(out.String()), "\n")
	return tests
}

func updateYAML(tests []string, jobName string, testPath string) {
	testsForYq := fmt.Sprintf("[%s]", strings.Join(tests, ", "))

	yqCommand := fmt.Sprintf(
		"yq eval '.jobs.%s.strategy.matrix.test = %s' %s -i",
		jobName,
		testsForYq,
		testPath,
	)
	cmd := exec.Command("bash", "-c", yqCommand)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Printf("stdout: %s", stdout.String())
		log.Printf("stderr: %s", stderr.String())
		log.Fatalf("failed to run yq command: %s", err)
	}

	fmt.Printf("YAML file (%s) job %s updated successfully\n", testPath, jobName)
}

func main() {
	tests := findTests()

	// Expand tests that should be split into multiple jobs
	expandedTests := expandTests(tests)

	quotedTests := make([]string, len(expandedTests))
	for i, test := range expandedTests {
		quotedTests[i] = fmt.Sprintf("\"%s\"", test)
	}

	// Define selected tests for PostgreSQL
	postgresTestNames := []string{
		"TestACLAllowUserDst",
		"TestPingAllByIP",
		"TestEphemeral2006DeletedTooQuickly",
		"TestPingAllByIPManyUpDown",
		"TestSubnetRouterMultiNetwork",
	}

	quotedPostgresTests := make([]string, len(postgresTestNames))
	for i, test := range postgresTestNames {
		quotedPostgresTests[i] = fmt.Sprintf("\"%s\"", test)
	}

	// Update both SQLite and PostgreSQL job matrices
	updateYAML(quotedTests, "sqlite", "./test-integration.yaml")
	updateYAML(quotedPostgresTests, "postgres", "./test-integration.yaml")
}
