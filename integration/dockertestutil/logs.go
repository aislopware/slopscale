package dockertestutil

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"github.com/ory/dockertest/v4"
)

const filePerm = 0o644

// WriteLog copies the container's whole log so far into stdout and stderr.
func WriteLog(
	pool *Pool,
	resource dockertest.Resource,
	stdout io.Writer,
	stderr io.Writer,
) error {
	logs, err := pool.Docker.ContainerLogs(context.Background(), resource.ID(), client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "all",
	})
	if err != nil {
		return err
	}
	defer logs.Close()

	_, err = stdcopy.StdCopy(stdout, stderr, logs)

	return err
}

func SaveLog(
	pool *Pool,
	resource dockertest.Resource,
	basePath string,
) (string, string, error) {
	err := os.MkdirAll(basePath, os.ModePerm)
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer

	err = WriteLog(pool, resource, &stdout, &stderr)
	if err != nil {
		return "", "", err
	}

	name := resource.Container().Name

	log.Printf("Saving logs for %s to %s\n", name, basePath)

	stdoutPath := path.Join(basePath, name+".stdout.log")

	err = os.WriteFile(
		stdoutPath,
		stdout.Bytes(),
		filePerm,
	)
	if err != nil {
		return "", "", err
	}

	stderrPath := path.Join(basePath, name+".stderr.log")

	err = os.WriteFile(
		stderrPath,
		stderr.Bytes(),
		filePerm,
	)
	if err != nil {
		return "", "", err
	}

	return stdoutPath, stderrPath, nil
}
