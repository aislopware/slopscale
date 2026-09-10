package main

//go:generate go run ./gh-action-integration-generator.go

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
)

// GitHub runs a limited number of this repository's jobs at once, so the
// matrix's wall clock is bounded by the total test time divided by that
// limit, not by the longest single test. One job per test spent that budget
// on setup: each job downloads four image tarballs and a Go cache before it
// runs anything, about a minute that 190-odd jobs paid 190 times.
//
// The tests are therefore packed into a fixed number of shards, sized to the
// lanes this matrix can actually have. Raise these together with the limit;
// leaving lanes idle costs wall clock, and asking for more lanes than exist
// only queues the surplus behind a full shard.
const (
	// concurrentJobLimit is how many jobs the account runs on
	// ubuntu-24.04-arm at once. Measured rather than assumed: sampling
	// every job of every workflow one push started, the peak was exactly
	// twenty.
	concurrentJobLimit = 20
	// siblingJobs is what the other workflows the same push starts hold
	// while this matrix wants to begin. The limit is shared, and the Go
	// workflow's lint, test, servertest and cross jobs run through the
	// first five minutes, which is exactly when the matrix starts. Sized
	// to the whole limit its last three shards queued behind them, the
	// last by two minutes, and that shard then finished last and set the
	// run's wall clock.
	//
	// Giving the lanes back costs nothing here. A shard's wall clock is
	// set by its longest indivisible leaf, 352 seconds of
	// TestAuthKeyLogoutAndReloginSameUser/with-https-false, and the
	// packer reports the same 5.9 minute maximum anywhere from fifteen
	// shards to nineteen. Fewer shards only fill the slack around that
	// leaf.
	siblingJobs    = 4
	postgresShards = 1
	sqliteShards   = concurrentJobLimit - siblingJobs - postgresShards
)

// streamsPerShard is how many `hi run` invocations a shard starts side by
// side. A test spends nearly all of its time waiting: sampled in CI, a
// control server averages under 5% of one CPU and 25 MB against a 1500 MB
// budget, so a shard's wall clock is set by how many tests it will run at
// once rather than by the runner's cores. Each stream is its own process
// with its own run id, which is what keeps the logs attributable and lets
// `hi` clean up only what it started.
const streamsPerShard = 3

// streamSeparator divides a shard's streams from each other. Patterns
// inside a stream are separated by spaces and run in order; the shard runs
// the streams concurrently and waits for all of them.
const streamSeparator = ";"

// durationsFile holds the measured runtime in seconds of every top-level
// test, which is what the shards are packed by. A split test may also carry
// a "Test/subtest-prefix" entry per leaf, which is what that leaf is packed
// by; without one the leaves share the whole test's time evenly. Refresh it
// from a real run when the balance drifts: the per-job times are in the
// run's job list, and a test missing from the file is packed as
// [defaultSeconds].
const durationsFile = "integration-test-durations.json"

// defaultSeconds is what a test not in [durationsFile] is assumed to cost,
// close to the median test. A new test lands in some shard either way; the
// only cost of a wrong guess is a less even split.
const defaultSeconds = 185

// testsToSplit defines tests that are split across shards by subtest.
// Key is the test function name, value is a list of subtest prefixes.
//
// A whole test cannot straddle two shards, so any test longer than a shard
// sets the wall clock on its own. [TestAutoApproveMultiNetwork] takes about
// 88 minutes, several times a shard, and splits into 24 independent leaves.
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

// postgresTests is the subset of tests that also runs against PostgreSQL.
var postgresTests = []string{
	"TestACLAllowUserDst",
	"TestPingAllByIP",
	"TestEphemeral2006DeletedTooQuickly",
	"TestPingAllByIPManyUpDown",
	"TestSubnetRouterMultiNetwork",
}

// item is one unit of work a shard can be given: a whole test, or a single
// subtest prefix of a test that is split.
type item struct {
	top     string
	sub     string
	seconds int
}

// shard is one matrix entry: a name for its logs and the streams of go test
// patterns it runs, separated by [streamSeparator].
type shard struct {
	Name  string `json:"name"`
	Tests string `json:"tests"`
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

func readDurations() map[string]int {
	raw, err := os.ReadFile(durationsFile)
	if err != nil {
		log.Fatalf("reading %s: %s", durationsFile, err)
	}

	var durations map[string]int

	err = json.Unmarshal(raw, &durations)
	if err != nil {
		log.Fatalf("parsing %s: %s", durationsFile, err)
	}

	return durations
}

// toItems turns test names into the units the shards are packed from. A split
// test contributes one item per subtest prefix, each carrying an even share of
// the whole test's measured time.
func toItems(tests []string, durations map[string]int) []item {
	var items []item

	for _, test := range tests {
		seconds, ok := durations[test]
		if !ok {
			seconds = defaultSeconds
		}

		prefixes, split := testsToSplit[test]
		if !split {
			items = append(items, item{top: test, seconds: seconds})

			continue
		}

		for _, prefix := range prefixes {
			// An even share is only a guess, and a bad one where the leaves
			// differ: TestAuthKeyLogoutAndReloginSameUser splits into 352
			// seconds of with-https-false and about 50 of with-https-true, so
			// halving its total put a six minute item in a shard budgeted
			// three. Use the measured leaf when the file carries one.
			share, ok := durations[test+"/"+prefix]
			if !ok {
				share = seconds / len(prefixes)
			}

			items = append(items, item{top: test, sub: prefix, seconds: share})
		}
	}

	return items
}

// pack distributes items over n shards longest-first, each going to the shard
// with the least work so far. Ties are broken by name so the output is stable
// and the workflow only changes when the tests or their timings do.
func pack(items []item, n int) [][]item {
	sorted := slices.Clone(items)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].seconds != sorted[j].seconds {
			return sorted[i].seconds > sorted[j].seconds
		}

		return sorted[i].top+sorted[i].sub < sorted[j].top+sorted[j].sub
	})

	bins := make([][]item, n)
	load := make([]int, n)

	for _, it := range sorted {
		best := 0
		for i := 1; i < n; i++ {
			if load[i] < load[best] {
				best = i
			}
		}

		bins[best] = append(bins[best], it)
		load[best] += it.seconds
	}

	return bins
}

// patterns renders one shard's items as go test -run patterns. Whole tests
// collapse into a single alternation; each split test needs its own pattern,
// because go test applies the part after "/" to every test the part before it
// selected.
func patterns(items []item) string {
	var whole []string

	subs := map[string][]string{}

	for _, it := range items {
		if it.sub == "" {
			whole = append(whole, it.top)

			continue
		}

		subs[it.top] = append(subs[it.top], it.sub)
	}

	var out []string

	if len(whole) > 0 {
		sort.Strings(whole)
		out = append(out, "^("+strings.Join(whole, "|")+")$")
	}

	for _, top := range slices.Sorted(maps.Keys(subs)) {
		sort.Strings(subs[top])
		// ".*" lets a prefix match the rest of the subtest name; the test
		// name is anchored so "…SameUser" does not also select
		// "…SameUserExpiredKey" and run it in two shards.
		out = append(out, "^"+top+"$/^("+strings.Join(subs[top], "|")+").*$")
	}

	return strings.Join(out, " ")
}

func shards(tests []string, durations map[string]int, n int) []shard {
	bins := pack(toItems(tests, durations), n)
	out := make([]shard, 0, len(bins))

	for i, bin := range bins {
		if len(bin) == 0 {
			continue
		}

		total := 0
		for _, it := range bin {
			total += it.seconds
		}

		var (
			streams []string
			longest int
		)

		for _, stream := range pack(bin, streamsPerShard) {
			if len(stream) == 0 {
				continue
			}

			load := 0
			for _, it := range stream {
				load += it.seconds
			}

			longest = max(longest, load)

			streams = append(streams, patterns(stream))
		}

		out = append(out, shard{
			Name:  fmt.Sprintf("%02d", i+1),
			Tests: strings.Join(streams, streamSeparator),
		})
		log.Printf(
			"shard %02d: %2d tests, %4.1f min in %d streams (%4.1f min serial)",
			i+1, len(bin), float64(longest)/60, len(streams), float64(total)/60,
		)
	}

	return out
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

	out, err := exec.Command(rgBin, args...).Output()
	if err != nil {
		log.Fatalf("failed to run command: %s", err)
	}

	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// updateYAML writes a job's shard matrix. The value goes through the
// environment rather than the expression, so a pattern's regex characters
// never reach a shell or yq's parser as syntax.
func updateYAML(entries []shard, jobName string, testPath string) {
	encoded, err := json.Marshal(entries)
	if err != nil {
		log.Fatalf("encoding shards: %s", err)
	}

	// The shards are handed over as JSON, which yq keeps in flow style with
	// every scalar double quoted. Collections become block style so oxfmt
	// leaves the file alone, and the scalars stay quoted so a pattern's
	// leading "^" is never read as YAML syntax. Without this the workflow
	// that regenerates and diffs could not agree with the formatted tree.
	const restyle = `(.jobs.%[1]s.strategy.matrix.shard | .. | ` +
		`select(tag == "!!map" or tag == "!!seq")) style="" | ` +
		`(.jobs.%[1]s.strategy.matrix.shard | .. | select(tag == "!!str")) style="double" | ` +
		`(.jobs.%[1]s.strategy.matrix.shard[][] | key) style=""`

	expr := fmt.Sprintf(".jobs.%[1]s.strategy.matrix.shard = env(SHARDS) | "+restyle, jobName)

	cmd := exec.Command("yq", "eval", expr, testPath, "-i")
	cmd.Env = append(os.Environ(), "SHARDS="+string(encoded))

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("yq: %s", output)
		log.Fatalf("failed to run yq: %s", err)
	}

	fmt.Printf("YAML file (%s) job %s updated with %d shards\n", testPath, jobName, len(entries))
}

func main() {
	durations := readDurations()
	tests := findTests()

	updateYAML(shards(tests, durations, sqliteShards), "sqlite", "./test-integration.yaml")
	updateYAML(shards(postgresTests, durations, postgresShards), "postgres", "./test-integration.yaml")
}
