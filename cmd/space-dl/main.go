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
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/spf13/pflag"

	spacedl "github.com/qitoi/space-dl"
)

const (
	METADATA_FILENAME = "metadata.txt"
	FILELIST_FILENAME = "files.txt"
)

func usage() {
	e, _ := os.Executable()
	e = path.Base(e)
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

	resp, err := getAudioSpaceInfo(client, spaceID)
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

	// save metadata
	metadata := path.Join(dir, METADATA_FILENAME)
	title := resp.Data.AudioSpace.Metadata.Title
	if err := saveMetadata(metadata, spaceID, title, u.DisplayName, startedAt); err != nil {
		return err
	}

	mediaKey := resp.Data.AudioSpace.Metadata.MediaKey
	streamURL, err := getStreamURL(client, mediaKey)
	if err != nil {
		return err
	}

	fmt.Printf("stream url: %s\n", streamURL)
	fmt.Println("start download")

	// download stream
	if err := download(client, spaceID, streamURL, dir); err != nil {
		return err
	}

	// save file list
	filelist := path.Join(dir, FILELIST_FILENAME)
	if err := saveFileList(filelist, dir); err != nil {
		return err
	}

	fmt.Println("merge files")

	// merge media files
	output := dir + ".m4a"
	if err := mergeFiles(output, filelist, metadata); err != nil {
		return fmt.Errorf("ffmpeg error: %w", err)
	}

	fmt.Println("done")

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

func download(client *spacedl.Client, spaceID, streamURL, dir string) error {
	dl := spacedl.NewDownloader(streamURL, dir)
	dl.Start(1 * time.Second)

	ticker := time.NewTicker(10 * time.Second)
loop:
	for {
		select {
		case <-ticker.C:
			resp, err := getAudioSpaceInfo(client, spaceID)
			if err != nil {
				fmt.Printf("space info error: %v\n", err)
			}
			if isSpaceEnded(resp) {
				break loop
			}
		}
	}

	dl.Close()

	return nil
}

func saveFileList(file string, input string) error {
	fis, err := ioutil.ReadDir(input)
	if err != nil {
		return err
	}

	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, fi := range fis {
		if path.Ext(fi.Name()) != ".aac" {
			continue
		}

		var parts []string
		for _, n := range strings.Split(fi.Name(), "'") {
			parts = append(parts, "'"+n+"'")
		}
		name := strings.Join(parts, "\\'")
		_, err := f.WriteString("file " + name + "\n")
		if err != nil {
			return err
		}
	}

	return nil
}

func mergeFiles(file string, filelist string, metadata string) error {
	opts := []string{
		"-safe", "0",
		"-f", "concat",
		"-i", filelist,
		"-i", metadata,
		"-map_metadata", "1",
		"-codec", "copy",
		"-y",
		file,
	}
	cmd := exec.Command("ffmpeg", opts...)

	fmt.Println(cmd.String())

	return cmd.Run()
}

func isSpaceAvailable(resp *spacedl.AudioSpaceByIDResponse) bool {
	return resp.Data.AudioSpace.Metadata.State == "Running" || resp.Data.AudioSpace.Metadata.State == "Ended"
}

func isSpaceEnded(resp *spacedl.AudioSpaceByIDResponse) bool {
	return resp.Data.AudioSpace.Metadata.State == "Ended"
}

func getAudioSpaceInfo(client *spacedl.Client, spaceID string) (*spacedl.AudioSpaceByIDResponse, error) {
	variables := spacedl.AudioSpaceByIDVariables{
		ID: spaceID,
	}
	var resp spacedl.AudioSpaceByIDResponse
	err := client.Query("AudioSpaceById", variables, &resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}
