package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/james-antill/mpb"
	"github.com/james-antill/mpb/decor"
)

func imgur2urls(url string) []string {
	url = filepath.Base(url)
	url = "https://imgur.com/r/" + url

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("%s: %v", "main", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("non-200 status: %s", resp.Status)
		log.Printf("%s: %v", "main", err)
		return nil
	}

	bbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%s: ReadAll: %v", "main", err)
		return nil
	}
	body := string(bbody)

	var urls []string
	// URLs look like:
	// <a class="image-list-link" href="/r/awww/3HOTBqh" data-page="0">
	// <img alt="" src="//i.imgur.com/3HOTBqhb.jpg" />
	// </a>
	// <a class="image-list-link" href="/r/awww/PHaUnQG" data-page="0">
	// "b" suffix in implies means thumbnail.
	prefix := "<img alt=\"\" src=\"//i.imgur.com/"
	suffix := "\" />"
	re := regexp.MustCompile(prefix + "([^\"]*)b[.]([^.\"]*)" + suffix)
	for _, imglist := range re.FindAllStringSubmatch(body, -1) {
		imgurl := "https://i.imgur.com/" + imglist[1] + "." + imglist[2]
		urls = append(urls, imgurl)
	}

	return urls
}

const downloaders = 16

type imgurData struct {
	Subreddit string
	URL       string
	stated    bool
	cached    bool
}

func main() {
	log.SetOutput(os.Stderr)

	var wg sync.WaitGroup
	p := mpb.New(mpb.WithWaitGroup(&wg))

	var args []string
	if len(os.Args) <= 1 {
		args = []string{"Aww"}
	} else {
		args = os.Args[1:]
	}

	done := make(chan bool)
	urls := make(chan *imgurData)
	wg.Add(downloaders)
	for i := 0; i < downloaders; i++ {
		go func() {
			for url := range urls {
				if cached(url) {
					continue
				}
				wg.Add(1)
				download(&wg, p, url)
				done <- true
			}
			wg.Done()
		}()
	}

	var surls []*imgurData
	ibar := p.AddBarDef(int64(len(args)), "Input: ", decor.Unit_k)
	for _, arg := range args {
		arg = filepath.Base(arg)
		for _, url := range imgur2urls(arg) {
			surls = append(surls, &imgurData{Subreddit: arg, URL: url})
		}
		ibar.Increment()
	}
	nurls := int64(len(surls))
	ubar := p.AddBarDef(nurls, "->URLs: ", decor.Unit_k, mpb.BarID(666))
	nuurls := int64(uncachedNum(surls))
	dbar := p.AddBarDef(nuurls, "<-URLs: ", decor.Unit_k, mpb.BarID(666))
	if false {
		go func() {
			dbar.Incr(0)
			for {
				select {
				case <-done:
					dbar.Increment()
				case <-time.After(500 * time.Millisecond):
					if dbar.Current() > 0 {
						dbar.Incr(0)
					}
				}
			}
		}()
	} else {
		go func() {
			for range done {
				dbar.Increment()
			}
		}()
	}
	for _, url := range surls {
		urls <- url
		ubar.Increment()
	}
	close(urls)

	p.Stop()
	close(done)
	fmt.Println("Finished")
}

func cached(url *imgurData) bool {
	dir := "imgur/" + url.Subreddit
	if url.stated {
		return url.cached
	}
	url.stated = true

	destName := filepath.Base(url.URL)
	if dir != "" {
		destName = dir + "/" + destName
	}
	if _, err := os.Stat(destName); err == nil {
		url.cached = true
		return true
	}

	return false
}

func uncachedNum(urls []*imgurData) int {
	num := 0
	for _, url := range urls {
		if cached(url) {
			continue
		}
		num++
	}
	return num
}

func download(wg *sync.WaitGroup, p *mpb.Progress, url *imgurData) {
	defer wg.Done()

	name := url.Subreddit + "/" + filepath.Base(url.URL) + ": "
	dir := "imgur/" + url.Subreddit

	destName := filepath.Base(url.URL)
	if dir != "" {
		destName = dir + "/" + destName
	}

	destDir := filepath.Dir(destName)
	if _, err := os.Stat(destName); err != nil {
		os.MkdirAll(destDir, os.ModePerm)
	}

	resp, err := http.Get(url.URL)
	if err != nil {
		log.Printf("%s: %v", name, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("non-200 status: %s", resp.Status)
		log.Printf("%s: %v", name, err)
		return
	}

	size := resp.ContentLength

	// create dest
	tdestName := destName + ".tmp"
	dest, err := os.Create(tdestName)
	if err != nil {
		err = fmt.Errorf("Can't create %s: %v", destName, err)
		log.Printf("%s: %v", name, err)
		return
	}

	// create bar with appropriate decorators
	bar := p.AddBarDef(size, name, decor.Unit_KiB)

	// create proxy reader
	reader := bar.ProxyReader(resp.Body)
	// and copy from reader
	_, err = io.Copy(dest, reader)

	if closeErr := dest.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Printf("%s: %v", name, err)
	} else {
		os.Rename(tdestName, destName)
	}
}
