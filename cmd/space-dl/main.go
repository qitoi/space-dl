/*
 *  Copyright 2021 qitoi
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/pflag"

	spacedl "github.com/qitoi/space-dl"
)

const (
	MetadataFilename = "metadata.txt"
)

var (
	errRe = regexp.MustCompile(`^The following (\w+) cannot be null: ([\w, ]+)$`)
)

func usage() {
	e, _ := os.Executable()
	e = filepath.Base(e)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s <space_id>\n", e)
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println(pflag.CommandLine.FlagUsages())
}

func main() {
	var check bool
	var help bool

	pflag.BoolVarP(&help, "help", "h", false, "help")
	pflag.BoolVar(&check, "check", false, "check ffmpeg")

	pflag.Parse()

	if help {
		usage()
		os.Exit(0)
	} else if check {
		if err := spacedl.CheckFFmpeg(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("OK: ffmpeg installed")
		os.Exit(0)
	} else if pflag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "invalid arguments")
		usage()
		os.Exit(1)
	}

	spaceID := os.Args[1]

	if err := run(spaceID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(spaceID string) error {
	client, _ := spacedl.NewClient()
	if err := client.Initialize(); err != nil {
		return err
	}

	params := buildAudioSpaceInfoParams(spaceID)

	resp, params, err := getAudioSpaceInfo(client, params)
	if err != nil {
		return err
	}

	if !isSpaceAvailable(resp) {
		return errors.New("space is not available")
	}

	u := spacedl.GetOwnerUser(resp)
	if u == nil {
		return errors.New("user not found")
	}

	startedAtUnix := resp.Data.AudioSpace.Metadata.StartedAt
	startedAt := time.Unix(startedAtUnix/1000, startedAtUnix%1000*1000000)
	dir := fmt.Sprintf("%s-%s", startedAt.Local().Format("20060102-150405"), u.TwitterScreenName)
	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}

	// create log
	logfile, err := os.Create(filepath.Join(dir, "space-dl.log"))
	if err != nil {
		return err
	}
	lw := io.MultiWriter(os.Stdout, logfile)
	logger := log.New(lw, "", log.LstdFlags)

	// save metadata
	metadata := filepath.Join(dir, MetadataFilename)
	title := resp.Data.AudioSpace.Metadata.Title
	if err := saveMetadata(metadata, spaceID, title, u.DisplayName, startedAt); err != nil {
		return err
	}

	mediaKey := resp.Data.AudioSpace.Metadata.MediaKey
	streamURL, err := getStreamURL(client, mediaKey)
	if err != nil {
		return err
	}

	logger.Printf("stream url: %s\n", streamURL)

	// download stream
	if err := download(client, params, streamURL, dir, logger); err != nil {
		return err
	}

	files, err := getSegmentFilePaths(dir)
	if err != nil {
		return err
	}

	// concatenate media files
	output := dir + ".m4a"
	if err := concatFiles(output, files, metadata, logger); err != nil {
		return fmt.Errorf("ffmpeg error: %w", err)
	}

	logger.Println("done")

	return nil
}

func saveMetadata(file string, spaceID, title, name string, startedAt time.Time) error {
	var meta spacedl.Metadata
	meta.Add("title", title)
	meta.Add("artist", name)
	meta.Add("date", startedAt.Local().Format("2006"))
	meta.Add("comment", fmt.Sprintf("https://twitter.com/i/spaces/%s", spaceID))

	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(meta.String()); err != nil {
		return err
	}

	return nil
}

func getStreamURL(client *spacedl.Client, mediaKey string) (string, error) {
	streamURL, err := client.GetStreamURL(mediaKey)
	if err != nil {
		return "", fmt.Errorf("stream url not found: %w", err)
	}
	return streamURL, nil
}

func download(client *spacedl.Client, params []spacedl.QueryParameter, streamURL, dir string, logger *log.Logger) error {
	dl := spacedl.NewDownloader(streamURL, dir)
	dl.Logger = logger

	dl.Start(1 * time.Second)

	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-ticker.C:
			resp, newParams, err := getAudioSpaceInfo(client, params)
			if err != nil {
				logger.Printf("space info error: %v\n", err)
				continue
			}
			params = newParams
			if isSpaceEnded(resp) {
				ticker.Stop()
				dl.Halt()
			}
		case <-dl.Done:
			return nil
		}
	}
}

func getSegmentFilePaths(dir string) ([]string, error) {
	fis, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, fi := range fis {
		if filepath.Ext(fi.Name()) != ".aac" {
			continue
		}

		p, err := filepath.Abs(filepath.Join(dir, fi.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, p)
	}

	return files, nil
}

func concatFiles(output string, files []string, metadata string, logger *log.Logger) error {
	opts := []string{
		"-i", "pipe:0",
		"-i", metadata,
		"-map_metadata", "1",
		"-codec", "copy",
		"-y",
		output,
	}
	cmd := exec.Command("ffmpeg", opts...)
	cmd.Stdout = logger.Writer()
	cmd.Stderr = cmd.Stdout

	logger.Printf("run: %s\n", cmd.String())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	ch := make(chan error)

	go func() {
		defer stdin.Close()
		defer close(ch)

		for _, input := range files {
			err := func() error {
				f, err := os.Open(input)
				if err != nil {
					return err
				}
				defer f.Close()
				if _, err = io.Copy(stdin, f); err != nil {
					return err
				}
				return nil
			}()
			if err != nil {
				ch <- err
				return
			}
		}
	}()

	for err := range ch {
		if err != nil {
			cmd.Process.Kill()
			return err
		}
	}

	return cmd.Wait()
}

func isSpaceAvailable(resp *spacedl.AudioSpaceByIDResponse) bool {
	return resp.Data.AudioSpace.Metadata.State == "Running" || resp.Data.AudioSpace.Metadata.State == "Ended"
}

func isSpaceEnded(resp *spacedl.AudioSpaceByIDResponse) bool {
	return resp.Data.AudioSpace.Metadata.State == "Ended"
}

func buildAudioSpaceInfoParams(spaceID string) []spacedl.QueryParameter {
	var params []spacedl.QueryParameter

	variables := spacedl.AudioSpaceByIDVariables{
		ID: spaceID,
	}
	v, _ := json.Marshal(variables)
	var vv map[string]interface{}
	json.Unmarshal(v, &vv)
	params = append(params, spacedl.QueryParameter{
		Name:  "variables",
		Value: vv,
	})

	features := spacedl.AudioSpaceByIDFeatures{}
	f, _ := json.Marshal(features)
	var ff map[string]interface{}
	json.Unmarshal(f, &ff)
	params = append(params, spacedl.QueryParameter{
		Name:  "features",
		Value: ff,
	})

	return params
}

func getAudioSpaceInfo(client *spacedl.Client, params []spacedl.QueryParameter) (*spacedl.AudioSpaceByIDResponse, []spacedl.QueryParameter, error) {
	var resp spacedl.AudioSpaceByIDResponse
	err := client.Query("AudioSpaceById", params, &resp)
	if qe, ok := err.(*spacedl.QueryError); ok {
		missingParam := false
		for _, e := range qe.Errors {
			fmt.Fprintf(os.Stderr, "AudioSpaceById query error: %v\n", e)
			matches := errRe.FindStringSubmatch(e.Message)
			if matches != nil {
				missingParam = true
				queryKey := matches[1]
				for _, paramKey := range strings.Split(matches[2], ", ") {
					params = appendMissingParam(params, queryKey, paramKey, false)
				}
			}
		}
		if missingParam {
			return getAudioSpaceInfo(client, params)
		}
		return nil, nil, err
	} else if err != nil {
		return nil, nil, err
	}

	return &resp, params, nil
}

func appendMissingParam(params []spacedl.QueryParameter, paramKey, key string, value interface{}) []spacedl.QueryParameter {
	p := params[:]
	done := false
	for idx := range p {
		if params[idx].Name == paramKey {
			params[idx].Value[key] = value
			done = true
			break
		}
	}
	if !done {
		p = append(p, spacedl.QueryParameter{
			Name: paramKey,
			Value: map[string]interface{}{
				key: value,
			},
		})
	}
	return params
}
