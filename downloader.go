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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/grafov/m3u8"
)

type Downloader struct {
	url    string
	output string
	seq    sync.Map

	close chan interface{}
	dlCh  chan *url.URL
	wg    sync.WaitGroup

	Parallel int
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
	d.close = make(chan interface{})
	d.dlCh = make(chan *url.URL, 10)

	// queue segment
	go func() {
		defer close(d.dlCh)
		ticker := time.NewTicker(interval)
	loop:
		for {
			select {
			case <-d.close:
				break loop
			case <-ticker.C:
				if urls, err := d.getSegments(); err != nil {
					fmt.Printf("playlist download error: %v\n", err)
				} else {
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
					fmt.Printf("segment download error (%v): %v\n", *u, err)
				}
			}
		}()
	}
}

func (d *Downloader) Close() {
	close(d.close)
	d.wg.Wait()
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
					fmt.Printf("url parse error: %v\n", err)
				}

				d.seq.Store(seg.SeqId, true)
				urls = append(urls, segURL)
			}
		}
	}

	return urls, nil
}

func (d *Downloader) downloadSegment(u *url.URL) error {
	if err := os.MkdirAll(d.output, 0777); err != nil {
		return err
	}

	// output file
	filename := path.Base(u.Path)
	p := path.Join(d.output, filename)
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