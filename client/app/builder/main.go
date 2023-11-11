package builder

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
)

var dockerRegistryUserID = ""

type ErrorLine struct {
	Error       string      `json:"error"`
	ErrorDetail ErrorDetail `json:"errorDetail"`
}

type ErrorDetail struct {
	Message string `json:"message"`
}

func Build(filePath string, imageName string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	err = imageBuild(cli, filePath, imageName)
	if err != nil {
		return err
	}

	return nil
}
func ImageExists(image string) (bool, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false, err
	}

	filters := filters.NewArgs()
	filters.Add("reference", image + ":latest")

	opts := types.ImageListOptions {
		Filters: filters,
	}

	imgs, err := cli.ImageList(context.Background(), opts)

	if len(imgs) == 1 {
		return true, nil
	}

	return false, nil
}

func imageBuild(dockerClient *client.Client, filePath string, imageName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*120)
	defer cancel()

	tar, err := archive.TarWithOptions(filePath, &archive.TarOptions{})
	if err != nil {
		return err
	}

	opts := types.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{dockerRegistryUserID + imageName},
		Remove:     true,
	}
	res, err := dockerClient.ImageBuild(ctx, tar, opts)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	err = print(res.Body)
	if err != nil {
		return err
	}

	return nil
}

func print(rd io.Reader) error {
	var lastLine string

	scanner := bufio.NewScanner(rd)
	for scanner.Scan() {
		lastLine = scanner.Text()
		fmt.Println(scanner.Text())
	}

	errLine := &ErrorLine{}
	json.Unmarshal([]byte(lastLine), errLine)
	if errLine.Error != "" {
		return errors.New(errLine.Error)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}