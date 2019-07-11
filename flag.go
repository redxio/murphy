package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

var (
	configFile    string
	proxyAddr     string
	prefix        string
	logFile       string
	depth, maxCon int
	external      bool
	headlessMode  bool
	ftypes        fileExts
	showVersion   bool
	disableCookie bool
)

type fileExts []string

func (mt *fileExts) String() string {
	return fmt.Sprintf("%s", *mt)
}

func (mt *fileExts) Set(value string) error {
	if value == "-" || value == "--" {
		return errors.New("empty flag value")
	}
	for _, str := range strings.Split(value, ",") {
		if len(str) > 0 {
			*mt = append(*mt, str)
		}
	}
	return nil
}

func init() {
	flag.BoolVar(&showVersion, "version", false, versionUsage)
	flag.StringVar(&configFile, "config", "", configUsage)
	flag.StringVar(&proxyAddr, "proxy", "", proxyUsage)
	flag.IntVar(&depth, "depth", 0, depthUsage)
	flag.IntVar(&maxCon, "maxCon", 0, maxConUsage)
	flag.BoolVar(&external, "external", false, externalUsage)
	flag.StringVar(&prefix, "dir", "", dirUsage)
	flag.StringVar(&logFile, "log", "", logUsage)
	flag.Var(&ftypes, "ftypes", listUsage)
	flag.BoolVar(&headlessMode, "headless", false, headlessUsage)
	flag.BoolVar(&disableCookie, "disableCookie", false, cookieUsage)

	flag.Usage = func() {
		progName := strings.TrimPrefix(os.Args[0], "./")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s: %s [-flags] <urls>\n", progName, progName)
		fmt.Fprintln(os.Stdout, notice)
		flag.PrintDefaults()
	}
	//userAgent = "Mozilla/5.0 (" + runtime.GOOS + " " + runtime.GOARCH + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/67.0.3396.99"
}
