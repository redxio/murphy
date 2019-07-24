package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/NzKSO/murphy/conf"
	"github.com/NzKSO/murphy/crawl"
	"github.com/NzKSO/murphy/headless"
)

func handleFlag() {
	flag.Parse()

	if showVersion {
		fmt.Println(versionInfo)
		os.Exit(0)
	}

	if flag.NArg() < 1 || flag.NFlag() < 1 {
		flag.Usage()
		os.Exit(1)
	}
}

func handleConfig(path string) (*conf.Configuration, error) {
	config, err := readConfigFile(configFile)
	if err != nil {
		return nil, err
	}

	if err = overrideConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

func start() {
	handleFlag()

	config, err := handleConfig(configFile)
	if err != nil {
		log.Fatalln(err)
	}

	if config.Option.Depth == 0 {
		return
	}

	crawler, err := newCrawler(config)
	if err != nil {
		log.Fatalln(err)
	}

	if config.Headless.Enable {
		server := headless.New()
		if err := server.Start(); err != nil {
			log.Fatalln(err)
		}
		defer server.Stop()
		crawler.Server = server
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	crawler.URLTopoCh = make(chan *crawl.URLTopological, 10)

	go func() {
		for _, rawurl := range flag.Args() {
			u, err := url.ParseRequestURI(rawurl)
			if err != nil {
				log.Fatalln(err)
			} else if u.Scheme != "http" && u.Scheme != "https" {
				log.Fatalln(invalidURL)
			}
			crawler.URLTopoCh <- &crawl.URLTopological{URL: u, Depth: config.Option.Depth}
		}
	}()

loop:
	for {
		select {
		case <-sigCh:
			break loop
		case urlTopo, ok := <-crawler.URLTopoCh:
			if !ok {
				break loop
			}

			if crawler.Semaphore != nil {
				crawler.Semaphore <- true
			}

			go crawler.Crawl(urlTopo)
		}
	}
}

func newCrawler(config *conf.Configuration) (*crawl.Crawler, error) {
	crawler := crawl.New(config)

	if config.Option.MaxConcurrency > 0 {
		crawler.Semaphore = make(chan bool, config.Option.MaxConcurrency)
	}

	if _, ok := crawl.MatchMIMEInExts("text/html", config.FileTypes); ok {
		crawler.DownloadHTML = true
	}

	if config.Dir == "" {
		config.Dir = "."
	}

	if err := os.MkdirAll(config.Dir, 0755); err != nil {
		return nil, err
	}

	if config.Log != "" {
		if err := os.MkdirAll(filepath.Dir(config.Log), 0755); err != nil {
			return nil, err
		}

		f, err := os.OpenFile(config.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0755)
		if err != nil {
			return nil, err
		}

		w := io.MultiWriter(f, os.Stdout)
		crawler.Logger = log.New(w, "", log.LstdFlags)
	} else {
		crawler.Logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	if !config.DisableCookie {
		crawler.EnableCookie()
	}

	if config.Proxy != nil {
		crawler.SetProxy(http.ProxyURL(config.Proxy.URL()))
	} else {
		crawler.SetProxy(http.ProxyFromEnvironment)
	}

	return crawler, nil
}
