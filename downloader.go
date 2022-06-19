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
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/grafov/m3u8"
)

const (
	playlistDownloadErrorLimit = 30
)

type Downloader struct {
	url    string
	output string
	seq    sync.Map

	halt chan struct{}
	dlCh chan *url.URL
	wg   sync.WaitGroup

	Parallel int
	Done     chan struct{}
	Logger   *log.Logger
}

func NewDownloader(url string, outputDir string) *Downloader {
	return &Downloader{
		url:      url,
		output:   outputDir,
		Parallel: 3,
	}
}

func (d *Downloader) Start(interval time.Duration) {
	d.seq = sync.Map{}
	d.Done = make(chan struct{})
	d.halt = make(chan struct{})
	d.dlCh = make(chan *url.URL, 10)

	// queue segment
	go func() {
		defer close(d.dlCh)
		errCount := 0
		ticker := time.NewTicker(interval)
	loop:
		for {
			select {
			case <-d.halt:
				break loop
			case <-ticker.C:
				if urls, err := d.getSegments(); err != nil {
					d.print("playlist download error: %v", err)
					errCount += 1
					if errCount > playlistDownloadErrorLimit {
						d.print("exceed error limit")
						d.Halt()
						break loop
					}
				} else {
					errCount = 0
					for _, u := range urls {
						d.dlCh <- u
					}
				}
			}
		}
	}()

	// download segment
	d.wg.Add(d.Parallel)
	for i := 0; i < d.Parallel; i++ {
		go func() {
			defer d.wg.Done()
			for u := range d.dlCh {
				if err := d.downloadSegment(u); err != nil {
					d.print("download error (%v): %v", *u, err)
				}
			}
		}()
	}

	go func() {
		d.wg.Wait()
		close(d.Done)
	}()
}

func (d *Downloader) Halt() {
	d.print("halt download")
	close(d.halt)
}

func (d *Downloader) getSegments() ([]*url.URL, error) {
	req, err := http.NewRequest(http.MethodGet, d.url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, err
	}

	// check playlist type
	if listType != m3u8.MEDIA {
		return nil, errors.New("invalid playlist")
	}
	mediaPlaylist, ok := playlist.(*m3u8.MediaPlaylist)
	if !ok {
		return nil, errors.New("invalid playlist")
	}

	u, err := url.Parse(d.url)
	if err != nil {
		return nil, err
	}

	var urls []*url.URL
	for _, seg := range mediaPlaylist.Segments {
		if seg != nil {
			if _, ok := d.seq.Load(seg.SeqId); !ok {
				segURL, err := u.Parse(seg.URI)
				if err != nil {
					d.print("url parse error: %v", err)
				}

				d.seq.Store(seg.SeqId, true)
				urls = append(urls, segURL)
			}
		}
	}

	return urls, nil
}

func (d *Downloader) downloadSegment(u *url.URL) error {
	d.print("download: %s", u.String())

	if err := os.MkdirAll(d.output, 0777); err != nil {
		return err
	}

	// output file
	filename := filepath.Base(u.Path)
	p := filepath.Join(d.output, filename)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func (d *Downloader) print(format string, v ...interface{}) {
	if d.Logger != nil {
		d.Logger.Printf(format+"\n", v...)
	}
}
