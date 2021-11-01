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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	spacedl "github.com/qitoi/space-dl"
)

var (
	ErrEmptySegment = errors.New("hls empty segment")
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
	var retryMax int
	var retryInterval int

	pflag.BoolVarP(&help, "help", "h", false, "help")
	pflag.BoolVar(&check, "check", false, "check ffmpeg")
	pflag.IntVar(&retryMax, "retry-max", 10, "retry max count")
	pflag.IntVar(&retryInterval, "retry-interval", 3, "retry interval time")

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

	if err := run(spaceID, retryMax, retryInterval); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(spaceID string, max int, interval int) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT)
	go func() {
		for {
			select {
			case <-ch:
				cancel()
			}
		}
	}()

	client, _ := spacedl.NewClient()
	if err := client.Initialize(); err != nil {
		return err
	}

	resp, err := getAudioSpaceInfo(client, spaceID)
	if err != nil {
		return err
	}

	u := spacedl.GetOwnerUser(resp)
	if u == nil {
		return errors.New("user not found")
	}

	mediaKey := resp.Data.AudioSpace.Metadata.MediaKey
	streamURL, err := client.GetStreamURL(mediaKey)
	if err != nil {
		return err
	}

	fmt.Printf("stream url: %s\n", streamURL)
	fmt.Println("start download")

	err = nil
	for trial := 0; trial < max; trial++ {
		fmt.Printf("download #%d\n", trial+1)

		err = download(ctx, streamURL, resp, u)
		if err != ErrEmptySegment {
			break
		}

		// hls empty segment, retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
			// retry
		}
	}

	if err != nil {
		return err
	}

	fmt.Println("done")

	return nil
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

	if resp.Data.AudioSpace.Metadata.State != "Running" {
		return nil, errors.New("space is not running")
	}

	return &resp, nil
}

func download(ctx context.Context, streamURL string, audioSpace *spacedl.AudioSpaceByIDResponse, u *spacedl.User) error {
	ffmpeg, logfile, err := setupDownload(streamURL, audioSpace, u)
	if err != nil {
		return err
	}
	defer logfile.Close()

	return execDownload(ctx, ffmpeg, logfile)
}

func setupDownload(streamURL string, audioSpace *spacedl.AudioSpaceByIDResponse, u *spacedl.User) (*spacedl.FFmpeg, *os.File, error) {
	spaceID := audioSpace.Data.AudioSpace.Metadata.RestID
	startedAtUnix := audioSpace.Data.AudioSpace.Metadata.StartedAt
	startedAt := time.Unix(startedAtUnix/1000, startedAtUnix%1000*1000000)

	basename := fmt.Sprintf("%s-%s-%d", u.TwitterScreenName, startedAt.Local().Format("20060102-150405"), time.Now().Unix())
	dst := basename + ".m4a"
	logFilename := basename + ".log"
	logfile, err := os.OpenFile(logFilename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, nil, err
	}

	metadata := map[string]string{
		"title":   audioSpace.Data.AudioSpace.Metadata.Title,
		"artist":  u.DisplayName,
		"date":    startedAt.Local().Format("2006"),
		"comment": fmt.Sprintf("https://twitter.com/i/spaces/%s", spaceID),
	}

	return spacedl.NewFFmpeg(streamURL, dst, metadata), logfile, nil
}

func execDownload(ctx context.Context, ffmpeg *spacedl.FFmpeg, logfile *os.File) error {
	err := ffmpeg.Download()
	if err != nil {
		return err
	}

	reader := io.TeeReader(ffmpeg.Reader, logfile)
	done := make(chan interface{})
	defer close(done)

	go func() {
	loop:
		for {
			select {
			case <-ctx.Done():
				fmt.Printf("stop\n")
				ffmpeg.Stop()
				break loop
			case <-done:
				break loop
			}
		}
	}()

	scanner := bufio.NewScanner(reader)
	var emptySegment bool
	for scanner.Scan() {
		if !emptySegment {
			s := scanner.Text()
			if strings.HasSuffix(s, "Empty segment") {
				emptySegment = true
				break
			}
		}
	}

	if err := ffmpeg.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			if emptySegment {
				return ErrEmptySegment
			}
		}
		return err
	}

	return nil
}
