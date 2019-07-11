package crawl

import (
	"context"
	"log"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NzKSO/murphy/conf"
	"github.com/NzKSO/murphy/headless"
	"golang.org/x/net/publicsuffix"
)

type URLTopological struct {
	URL   *url.URL
	Depth int
}

// Crawler ...
type Crawler struct {
	client       *http.Client
	crawledURL   *sync.Map
	counter      int64
	config       *conf.Configuration
	sem          chan bool
	URLTopoCh    chan *URLTopological
	Logger       *log.Logger
	Server       *headless.Server
	DownloadHTML bool
}

// New ...
func New(config *conf.Configuration) *Crawler {
	crawler := &Crawler{
		client:     &http.Client{},
		crawledURL: &sync.Map{},
		config:     config,
	}

	if config.Option.MaxConcurrency > 0 {
		crawler.sem = make(chan bool, config.Option.MaxConcurrency)
	}

	return crawler
}

// Crawl ...
func (crawler *Crawler) Crawl(urlTopo *URLTopological) {
	defer func() {
		if crawler.sem != nil && len(crawler.sem) > 0 {
			<-crawler.sem
		}
		
		if atomic.AddInt64(&crawler.counter, -1) == 0 && len(crawler.URLTopoCh) == 0 {
			close(crawler.URLTopoCh)
		}
	}()

	if crawler.sem != nil {
		crawler.sem <- true
	}
	atomic.AddInt64(&crawler.counter, 1)

	dir := filepath.Join(crawler.config.Dir, urlTopo.URL.Hostname())
	os.Mkdir(dir, 0755)

	rooturl := urlTopo.URL.String()
	crawler.crawledURL.LoadOrStore(rooturl, true)

	if crawler.config.Headless.Enable {
		resp, err := http.Head(rooturl)
		if err != nil {
			crawler.Logger.Printf("URL: %s, HTTP HEAD error: %v\n", rooturl, err)
			return
		}
		defer resp.Body.Close()

		MIME, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if MIME == "text/html" {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(crawler.config.Headless.Timeout)*time.Second)
			defer cancel()

			HTMLContent, err := crawler.Server.GetWebpageSourceCode(ctx, rooturl)
			if err != nil {
				crawler.Logger.Printf("URL: %s, GetWebpageSourceCode error: %v\n", rooturl, err)
				return
			}

			crawler.parseHTML(strings.NewReader(HTMLContent), urlTopo, dir)
			return
		}
	}

	req, err := http.NewRequest(http.MethodGet, rooturl, nil)
	if err != nil {
		crawler.Logger.Printf("URL: %s, NewRequest error: %v\n", rooturl, err)
		return
	}

	req.Header.Set("Accept-Charset", "utf-8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", crawler.config.UserAgent)

	resp, err := crawler.client.Do(req)
	if err != nil {
		crawler.Logger.Printf("URL: %s, HTTP GET error: %v\n", rooturl, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		crawler.Logger.Printf("URL: %s, status text: %v\n", rooturl, http.StatusText(resp.StatusCode))
		return
	}

	if !resp.Uncompressed {
		resp.Body, err = uncompressBody(resp)
		if err != nil {
			crawler.Logger.Printf("URL: %s, uncompress error: %v\n", rooturl, err)
			return
		}
	}

	MIME, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		crawler.Logger.Printf("URL: %s, ParseMediaType error: %v\n\n", rooturl, err)
		return
	}

	switch MIME {
	case "application/octet-stream":
		crawler.handleOctetStream(resp, dir)
	case "text/html":
		rd, err := convertToUnescapedUTF8Body(resp, params)
		if err != nil {
			crawler.Logger.Printf("URL: %s, Error converting to UTF8: %v", rooturl, err)
			return
		}

		crawler.parseHTML(rd, urlTopo, dir)
	default:
		if ext, ok := MatchMIMEInExts(MIME, crawler.config.FileTypes); ok {
			fileName := getFileName(urlTopo.URL.Path, ext)
			crawler.Logger.Printf("Found file %s on %v", fileName, resp.Request.URL.String())
			if err := writeFile(resp.Body, dir, fileName); err != nil {
				crawler.Logger.Printf("Error writing file %s: %v", fileName, err)
			}
		}
	}
}

// SetProxy sets proxy for crawler
func (crawler *Crawler) SetProxy(proxy func(*http.Request) (*url.URL, error)) {
	crawler.client.Transport = &http.Transport{Proxy: proxy}
}

// EnableCookie ...
func (crawler *Crawler) EnableCookie() {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		crawler.Logger.Println(err)
		return
	}
	crawler.client.Jar = jar
}

// SetTimeout ...
func (crawler *Crawler) SetTimeout(duration time.Duration) {
	crawler.client.Timeout = duration
}
