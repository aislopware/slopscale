package cli

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/mod/semver"
)

var cfgFile string

func init() {
	if len(os.Args) > 1 &&
		(os.Args[1] == "version" || os.Args[1] == "mockoidc" || os.Args[1] == "completion") {
		return
	}

	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().
		StringVarP(&cfgFile, "config", "c", "", "config file (default is /etc/headscale/config.yaml)")
	rootCmd.PersistentFlags().
		StringP("output", "o", "", "Output format. Empty for human-readable, 'json', 'json-line' or 'yaml'")
	rootCmd.PersistentFlags().
		Bool("force", false, "Disable prompts and forces the execution")

	// Re-enable usage output only for flag-parsing errors; runtime errors
	// from [cobra.Command.RunE] should never dump usage text.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		cmd.SilenceUsage = false

		return err
	})
}

func initConfig() {
	if cfgFile == "" {
		cfgFile = os.Getenv("HEADSCALE_CONFIG")
	}

	if cfgFile != "" {
		err := types.LoadConfig(cfgFile, true)
		if err != nil {
			log.Fatal().Caller().Err(err).Msgf("error loading config file %s", cfgFile)
		}
	} else {
		err := types.LoadConfig("", false)
		if err != nil {
			log.Fatal().Caller().Err(err).Msgf("error loading config")
		}
	}

	machineOutput := hasMachineOutputFlag()

	// If the user has requested a "node" readable format,
	// then disable login so the output remains valid.
	if machineOutput {
		zerolog.SetGlobalLevel(zerolog.Disabled)
	}

	logFormat := viper.GetString("log.format")
	if logFormat == types.JSONLogFormat {
		log.Logger = log.Output(os.Stdout)
	}

	disableUpdateCheck := viper.GetBool("disable_check_updates")
	if !disableUpdateCheck && !machineOutput {
		versionInfo := types.GetVersionInfo()
		if (runtime.GOOS == "linux" || runtime.GOOS == "darwin") &&
			!versionInfo.Dirty {
			newest, err := latestRelease(
				context.Background(),
				releasesURL,
				filterPreReleasesIfStable(func() string { return versionInfo.Version }),
			)
			if err == nil && isOutdated(versionInfo.Version, newest) {
				log.Warn().Msgf(
					"An updated version of Headscale has been found (%s vs. your current %s). "+
						"Check it out https://github.com/juanfont/headscale/releases\n",
					newest,
					versionInfo.Version,
				)
			}
		}
	}
}

const (
	releasesURL          = "https://api.github.com/repos/juanfont/headscale/releases?per_page=30"
	releaseCheckTimeout  = 5 * time.Second
	releaseCheckMaxBytes = 1 << 20
)

var errNoRelease = errors.New("no release found")

// latestRelease returns the newest release tag listed at url that skip does
// not reject, comparing tags as semantic versions.
func latestRelease(ctx context.Context, url string, skip func(tag string) bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, releaseCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("building release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching releases: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching releases: %w: %s", errNoRelease, resp.Status)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}

	err = json.UnmarshalRead(http.MaxBytesReader(nil, resp.Body, releaseCheckMaxBytes), &releases)
	if err != nil {
		return "", fmt.Errorf("decoding releases: %w", err)
	}

	newest := ""

	for _, release := range releases {
		tag := canonicalVersion(release.TagName)
		if release.Draft || !semver.IsValid(tag) || skip(release.TagName) {
			continue
		}

		if newest == "" || semver.Compare(tag, newest) > 0 {
			newest = tag
		}
	}

	if newest == "" {
		return "", errNoRelease
	}

	return newest, nil
}

// isOutdated reports whether newest is a higher semantic version than current.
func isOutdated(current, newest string) bool {
	current = canonicalVersion(current)
	if !semver.IsValid(current) {
		return false
	}

	return semver.Compare(canonicalVersion(newest), current) > 0
}

// canonicalVersion gives a tag the leading "v" that semver expects.
func canonicalVersion(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag
	}

	return "v" + tag
}

var prereleases = []string{"alpha", "beta", "rc", "dev"}

func isPreReleaseVersion(version string) bool {
	return slices.ContainsFunc(prereleases, func(unstable string) bool {
		return strings.Contains(version, unstable)
	})
}

// filterPreReleasesIfStable returns a function that filters out
// pre-release tags if the current version is stable.
// If the current version is a pre-release, it does not filter anything.
// versionFunc is a function that returns the current version string, it is
// a func for testability.
func filterPreReleasesIfStable(versionFunc func() string) func(string) bool {
	return func(tag string) bool {
		version := versionFunc()

		// If we are on a pre-release version, then we do not filter anything
		// as we want to recommend the user the latest pre-release.
		if isPreReleaseVersion(version) {
			return false
		}

		// If we are on a stable release, filter out pre-releases.
		return isPreReleaseVersion(tag)
	}
}

var rootCmd = &cobra.Command{
	Use:   "headscale",
	Short: "headscale - a Tailscale control server",
	Long: `
headscale is an open source implementation of the Tailscale control server

https://github.com/juanfont/headscale`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func Execute() {
	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		outputFormat, _ := cmd.Flags().GetString("output")
		printError(err, outputFormat)
		os.Exit(1)
	}
}
