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

package spacedl

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

type FFmpeg struct {
	cmd *exec.Cmd
}

func CheckFFmpeg() error {
	cmd := exec.Command("ffmpeg", "-version")
	return cmd.Run()
}

func (f *FFmpeg) Download(src, dst string, metadata map[string]string, logfile *os.File) error {
	opts := []string{"-i", src, "-c", "copy", "-bsf:a", "aac_adtstoasc"}

	for k, v := range metadata {
		opts = append(opts, "-metadata", k+"="+v)
	}

	opts = append(opts, dst)

	f.cmd = createCommand("ffmpeg", opts...)

	if logfile != nil {
		f.cmd.Stdout = logfile
		f.cmd.Stderr = logfile
		fmt.Fprintln(logfile, f.cmd.String())
		defer logfile.Sync()
	}

	return f.cmd.Start()
}

func (f *FFmpeg) Wait() error {
	return f.cmd.Wait()
}

func (f *FFmpeg) Stop() error {
	return f.cmd.Process.Signal(syscall.SIGINT)
}
