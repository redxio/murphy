package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
)

const (
	versionInfo     = "v1.0.0 beta"
	proxyUsage      = "Specify proxy address, currenctly support http(s) and socks5 proxy only"
	configUsage     = "Path to the configuration file"
	depthUsage      = "The depth of crawling webpage. 0 means that doesn't crawl any source, 1 indicates that crawling the url passed as command-line arg only, -1 means no limit (default 0)"
	maxConUsage     = "The maximum number of concurrency that allowed. 0 means no limit (default 0)"
	listUsage       = "Comma-separated without white space added list of file type to grab, e.g., jpeg,mp4,jpg,png"
	externalUsage   = "Enable crawling external webpage (default false)"
	versionUsage    = "Display version info for program"
	prefixUsage     = "Specify the directory where the file is saved"
	headlessUsage   = "Enable headless mode, require Chrome browser 59+ to be installed on your system"
	logUsage        = "Path to the log file"
	emptyListSet    = "File types can't be empty set! please use -ftypes to set or specify in configuration file and try again"
	invalidData     = "Invalid value, please check your configuration file and try again"
	invalidDepth    = "Invalid value, please provide an integer value greater than -2 and try again"
	invalidMaxCon   = "Invalid value, please provide an integer value greater than or equal to 0 and try again"
	invalidftypes   = "Invalid value, please provide a non-empty comma-separated without white space added list of file types and try again"
	notValidHTTPURL = "Not valid HTTP/HTTPS URL"
	unknownField    = "Unknow field, skipped. please check you configuration file"
	unsupportProxy  = "Unsupported proxy type, currenctly support http(s) and socks5 proxy only"
	notice          = "If flag valus provided, the corresponding settings in config file will be override"
	timeLayout      = "2006/01/02 15:04:05 MST"
	characterSet    = "_-9876543210ABCDEFGHIJKMNLOPQRSTUVWXYZzyxwvutsrqpolnmkjihgfedcba"
	logBufSize      = 100 * 1024
	flushSize       = logBufSize / 10
)

type proxy struct {
	protocol string
	port     int
	address  string
}

type option struct {
	depth            int
	maxConcurrency   int
	externalWebpages bool
}

type configuration struct {
	log       string
	prefix    string
	fileTypes []string
	pro       *proxy
	opt       option
	headless  bool
}

type urlTopological struct {
	url   *url.URL
	depth int
}

type crawler struct {
	client       *http.Client
	config       *configuration
	rootURL      *url.URL
	urlTopoCh    chan<- *urlTopological
	downloadHTML bool
}

type output struct {
	msg  interface{}
	exit bool
}

var (
	configFile    string
	proxyAddr     string
	prefix        string
	logFile       string
	depth, maxCon int
	external      bool
	headlessMode  bool
	ftypes        fileExts
	showVersion   *bool
)

var (
	crawledURL = &sync.Map{}
	outputCh   = make(chan *output)
	counter    int64
	bufw       *bufio.Writer
	wg         sync.WaitGroup
	userAgent  string
)

type fileExts []string

func (mt *fileExts) String() string {
	return fmt.Sprintf("%s", *mt)
}

func (mt *fileExts) Set(value string) error {
	if value == "-" || value == "--" {
		return errors.New("Empty flag value")
	}
	for _, str := range strings.Split(value, ",") {
		if len(str) > 0 {
			*mt = append(*mt, str)
		}
	}
	return nil
}

func init() {
	showVersion = flag.Bool("version", false, versionUsage)
	flag.StringVar(&configFile, "config", "", configUsage)
	flag.StringVar(&proxyAddr, "proxy", "", proxyUsage)
	flag.IntVar(&depth, "depth", 0, depthUsage)
	flag.IntVar(&maxCon, "maxCon", 0, maxConUsage)
	flag.BoolVar(&external, "external", false, externalUsage)
	flag.StringVar(&prefix, "dir", "", prefixUsage)
	flag.StringVar(&logFile, "log", "", logUsage)
	flag.Var(&ftypes, "ftypes", listUsage)
	flag.BoolVar(&headlessMode, "headless", false, headlessUsage)
	flag.Parse()

	flag.Usage = func() {
		progName := strings.TrimPrefix(os.Args[0], "./")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s: %s [-flags] <urls>\n", progName, progName)
		fmt.Fprintln(os.Stdout, notice)
		flag.PrintDefaults()
	}
	//userAgent = "Mozilla/5.0 (X11; Fedora; Linux x86_64; rv:61.0) Gecko/20100101 Firefox/61.0"
	userAgent = "Mozilla/5.0 (" + runtime.GOOS + " " + runtime.GOARCH + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/67.0.3396.99"
}
