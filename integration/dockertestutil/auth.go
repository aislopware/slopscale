package dockertestutil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/moby/moby/client"
)

const dockerHubServer = "https://index.docker.io/v1/"

type CredentialSource string

const (
	CredentialSourceEnv       CredentialSource = "env"
	CredentialSourceConfig    CredentialSource = "config"
	CredentialSourceAnonymous CredentialSource = "anonymous"
)

// Credentials resolves Docker Hub credentials from
// DOCKERHUB_USERNAME/DOCKERHUB_TOKEN, then ~/.docker/config.json, then
// anonymous. The Docker Go SDKs do not read config.json on their own.
func Credentials() (string, string, CredentialSource) {
	if u := os.Getenv("DOCKERHUB_USERNAME"); u != "" {
		return u, os.Getenv("DOCKERHUB_TOKEN"), CredentialSourceEnv
	}

	user, pass, ok := credentialsFromConfig()
	if ok {
		return user, pass, CredentialSourceConfig
	}

	return "", "", CredentialSourceAnonymous
}

// RegistryAuth returns base64-encoded credentials for the modern
// Docker SDK's image.PullOptions{RegistryAuth: ...}, or "" when none.
func RegistryAuth() (string, error) {
	u, p, _ := Credentials()
	if u == "" && p == "" {
		return "", nil
	}

	auth := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{Username: u, Password: p}

	b, err := json.Marshal(auth)
	if err != nil {
		return "", fmt.Errorf("marshalling docker auth: %w", err)
	}

	return base64.URLEncoding.EncodeToString(b), nil
}

// PullWithAuth ensures imageRef is local, pulling with auth and
// retrying transient errors when it is not.
func PullWithAuth(pool *Pool, imageRef string) error {
	ctx := context.Background()

	_, err := pool.Docker.ImageInspect(ctx, imageRef)
	if err == nil {
		return nil
	}

	registryAuth, err := RegistryAuth()
	if err != nil {
		return err
	}

	_, err = backoff.Retry(
		ctx,
		func() (struct{}, error) {
			pullErr := pullImage(ctx, pool.Docker, imageRef, registryAuth)
			if pullErr == nil {
				return struct{}{}, nil
			}

			if isPermanentPullError(pullErr) {
				return struct{}{}, backoff.Permanent(pullErr)
			}

			return struct{}{}, fmt.Errorf("pulling %s: %w", imageRef, pullErr)
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(60*time.Second),
	)
	if err != nil {
		return fmt.Errorf("pulling %s with auth (registry=%s): %w", imageRef, dockerHubServer, err)
	}

	return nil
}

// pullImage pulls imageRef and waits for the daemon to finish.
func pullImage(ctx context.Context, docker *client.Client, imageRef, registryAuth string) error {
	resp, err := docker.ImagePull(ctx, imageRef, client.ImagePullOptions{RegistryAuth: registryAuth})
	if err != nil {
		return err
	}
	defer resp.Close()

	return resp.Wait(ctx)
}

func isPermanentPullError(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "manifest unknown") ||
		strings.Contains(msg, "manifest not found") ||
		strings.Contains(msg, "repository does not exist") ||
		strings.Contains(msg, "name unknown") ||
		strings.Contains(msg, "no such image")
}

// credentialsFromConfig reads the Hub entry from ~/.docker/config.json.
// Credential helpers (osxkeychain etc.) are not supported; use env vars.
func credentialsFromConfig() (string, string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}

	raw, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return "", "", false
	}

	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}

	err = json.Unmarshal(raw, &cfg)
	if err != nil {
		return "", "", false
	}

	entry, found := cfg.Auths[dockerHubServer]
	if !found || entry.Auth == "" {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		return "", "", false
	}

	user, pass, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return "", "", false
	}

	return user, pass, true
}
