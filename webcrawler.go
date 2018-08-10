package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/NzKSO/murphy/headless"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"

	"golang.org/x/text/encoding/unicode"

	"github.com/fatih/color"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/publicsuffix"
)

func outputMsg() {
	defer wg.Done()
	var str string
	for out := range outputCh {
		switch v := out.msg.(type) {
		case error:
			str = v.Error()
		case string:
			str = v
		}
		str = "[" + time.Now().Format(timeLayout) + "] " + str + "\n"
		if bufw != nil {
			bufw.WriteString(str)
			if bufw.Available() < flushSize {
				bufw.Flush()
			}
		}
		color.New(color.FgRed, color.Bold).Print(str)
		if out.exit {
			if bufw != nil {
				bufw.Flush()
			}
			os.Exit(2)
		}
	}
	if bufw != nil {
		bufw.Flush()
	}
}

func getNextToken(dec *json.Decoder, token *json.Token) bool {
	for {
		tk, err := dec.Token()
		if err == io.EOF {
			return false
		} else if err != nil {
			outputCh <- &output{err, true}
		}

		if v, ok := tk.(json.Delim); ok {
			if v == json.Delim('{') || v == json.Delim('[') {
				continue
			} else if v == json.Delim('}') || v == json.Delim(']') {
				return false
			}
		}
		*token = tk
		return true
	}
}

func parseJSONFile(r io.Reader, config *configuration) error {
	var token json.Token
	dec := json.NewDecoder(r)

	var parseKey = true
	var key string
	for getNextToken(dec, &token) {
		if parseKey {
			key = token.(string)
			parseKey = false
			if key != "proxy" && key != "option" && key != "fileTypes" {
				continue
			}
		}
		parseKey = true

		switch key {
		case "log":
			log, ok := token.(string)
			if !ok {
				return errors.New("Field log: " + invalidData)
			}
			config.log = log
		case "dir":
			prefix, ok := token.(string)
			if !ok {
				return errors.New("Field dir: " + invalidData)
			}
			config.prefix = prefix
		case "proxy":
			config.pro = new(proxy)
			var setPort, setAddress bool
			for getNextToken(dec, &token) {
				if parseKey {
					key = token.(string)
					parseKey = false
					continue
				}
				parseKey = true

				switch key {
				case "port":
					port, ok := token.(float64)
					if !ok || (ok && (int(port) < 0 || int(port) > 65535)) {
						return errors.New("Field port: " + invalidData)
					}
					config.pro.port = int(port)
					setPort = true
				case "protocol":
					proto, ok := token.(string)
					if !ok {
						return errors.New("Field protocol: " + invalidData)
					}
					config.pro.protocol = proto
				case "address":
					addr, ok := token.(string)
					if !ok || (ok && net.ParseIP(addr) == nil) {
						return errors.New("Field address: " + invalidData)
					}
					config.pro.address = addr
					setAddress = true
				default:
					outputCh <- &output{fmt.Sprintf("%s: %s", key, unknownField), false}
				}
			}
			if !setPort {
				return errors.New("Missing proxy port in config file")
			}
			if !setAddress {
				return errors.New("Missing proxy address in config file")
			}
		case "fileTypes":
			for getNextToken(dec, &token) {
				extName, ok := token.(string)
				if !ok {
					return errors.New("Field fileTypes: " + invalidData)
				}
				if len(extName) > 0 {
					config.fileTypes = append(config.fileTypes, extName)
				}
			}
		case "option":
			for getNextToken(dec, &token) {
				if parseKey {
					key = token.(string)
					parseKey = false
					continue
				}
				parseKey = true

				switch key {
				case "depth":
					depth, ok := token.(float64)
					if !ok || (ok && int(depth) < -1) {
						return errors.New("Field depth: " + invalidDepth)
					}
					config.opt.depth = int(depth)
				case "maxConcurrency":
					maxCon, ok := token.(float64)
					if !ok || (ok && int(maxCon) < 0) {
						return errors.New("Field maxConcurrency: " + invalidMaxCon)
					}
					config.opt.maxConcurrency = int(maxCon)
				case "externalWebpages":
					external, ok := token.(bool)
					if !ok {
						return errors.New("Field externalWebpages: " + invalidData)
					}
					config.opt.externalWebpages = external
				default:
					outputCh <- &output{fmt.Sprintf("%s: %s", key, unknownField), false}
				}
			}
		case "headless":
			headless, ok := token.(bool)
			if !ok {
				return errors.New("Field headless: " + invalidData)
			}
			config.headless = headless
		default:
			outputCh <- &output{fmt.Sprintf("%s: %s", key, unknownField), false}
		}
	}
	return nil
}

func (p *proxy) URL() *url.URL {
	u := url.URL{
		Scheme: p.protocol,
		Path:   net.JoinHostPort(p.address, strconv.Itoa(p.port)),
	}
	return &u
}

func overrideConfig(config *configuration) error {
	var xFlag string
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i][0] == '-' {
			if len(os.Args[i]) == 1 || (os.Args[i][1] == '-' && len(os.Args[i]) == 2) {
				break
			}
			if os.Args[i][1] != '-' {
				xFlag = os.Args[i][1:]
			} else {
				xFlag = os.Args[i][2:]
			}
		} else {
			break
		}
		if idx := strings.IndexByte(xFlag, '='); idx > 0 {
			xFlag = xFlag[:idx]
		} else if xFlag != "external" && xFlag != "headless" {
			i++
		}
		switch xFlag {
		case "proxy":
			u, err := url.ParseRequestURI(proxyAddr)
			if err != nil {
				return err
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil {
				return err
			}
			if config.pro == nil {
				config.pro = new(proxy)
			}
			config.pro.address = u.Hostname()
			config.pro.port = port
			config.pro.protocol = u.Scheme
		case "depth":
			if depth < -1 {
				return errors.New("Flag depth: " + invalidDepth)
			}
			config.opt.depth = depth
		case "maxCon":
			if maxCon < 0 {
				return errors.New("Flag maxCon: " + invalidMaxCon)
			}
			config.opt.maxConcurrency = maxCon
		case "ftypes":
			if len(ftypes) < 1 {
				return errors.New("Flag ftypes: " + invalidftypes)
			}
			config.fileTypes = []string(ftypes)
		case "external":
			config.opt.externalWebpages = external
		case "dir":
			config.prefix = prefix
		case "log":
			config.log = logFile
		case "headless":
			config.headless = headlessMode
		}
	}
	if len(config.fileTypes) < 1 {
		return errors.New(emptyListSet)
	}
	return nil
}

func readConfigFile(pconfig *configuration, configFile string) error {
	f, err := os.Open(configFile)
	if err != nil {
		return err
	}
	defer f.Close()

	bufr := bufio.NewReader(f)
	fi, err := os.Stat(configFile)
	if os.IsNotExist(err) {
		return err
	}
	data, err := bufr.Peek(int(fi.Size()))
	if err != nil {
		return err
	}

	if !json.Valid(data) {
		return errors.New(invalidData)
	}
	if err = parseJSONFile(bufr, pconfig); err != nil {
		return err
	}
	return nil
}

func main() {
	if *showVersion {
		fmt.Println(versionInfo)
		os.Exit(0)
	}

	if flag.NArg() < 1 || flag.NFlag() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	wg.Add(1)
	go outputMsg()

	config := &configuration{}
	if configFile != "" {
		if err := readConfigFile(config, configFile); err != nil {
			outputCh <- &output{err, true}
		}
	}
	if err := overrideConfig(config); err != nil {
		outputCh <- &output{err, true}
	}
	if config.opt.depth == 0 {
		return
	}
	if config.log != "" {
		f, err := os.OpenFile(config.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0755)
		if err != nil {
			outputCh <- &output{err, true}
		}
		bufw = bufio.NewWriterSize(f, logBufSize)
		defer bufw.Flush()
	}

	var server *headless.Server
	if config.headless {
		server = headless.New()
		if err := server.Start(); err != nil {
			outputCh <- &output{err, true}
		}
		defer server.Stop()
	}

	transport := &http.Transport{}
	if err := setProxy(transport, config.pro); err != nil {
		outputCh <- &output{err, false}
	}
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		outputCh <- &output{err, true}
	}
	client := &http.Client{
		Transport: transport,
		Jar:       jar,
	}

	urlTopoCh := make(chan *urlTopological, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var invalid int

		for _, rawurl := range flag.Args() {
			u, err := url.ParseRequestURI(rawurl)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				if err != nil {
					outputCh <- &output{err, false}
				} else {
					outputCh <- &output{notValidHTTPURL, false}
				}
				invalid++
				if invalid == len(flag.Args()) {
					close(urlTopoCh)
					return
				}
				continue
			}
			if u.Fragment != "" {
				u.Fragment = ""
			}
			urlString := u.String()
			if _, loaded := crawledURL.LoadOrStore(urlString, true); !loaded {
				outputCh <- &output{"Fetching " + urlString, false}
				urlTopoCh <- &urlTopological{u, config.opt.depth}
			}
		}
	}()

	var downloadHTML bool
	if _, ok := matchExtsWithMIME("text/html", config.fileTypes); ok {
		downloadHTML = true
	}

	sem := make(chan bool, config.opt.maxConcurrency)
loop:
	for {
		select {
		case <-ch:
			if bufw != nil {
				bufw.Flush()
			}
			server.Stop()
			os.Exit(3)
		case urlTopo, ok := <-urlTopoCh:
			if !ok {
				break loop
			}
			if config.opt.maxConcurrency == 0 {
				atomic.AddInt64(&counter, 1)
				crawler := &crawler{client,
					config,
					urlTopo.url,
					urlTopoCh,
					downloadHTML,
				}
				go crawl(crawler, urlTopo.depth, server)
			} else {
				sem <- true
				atomic.AddInt64(&counter, 1)
				go func(urlTopo *urlTopological) {
					defer func() {
						if len(sem) > 0 {
							<-sem
						}
					}()
					crawler := &crawler{client,
						config,
						urlTopo.url,
						urlTopoCh,
						downloadHTML,
					}
					crawl(crawler, urlTopo.depth, server)
				}(urlTopo)
			}
		}
	}

	outputCh <- &output{"Exiting program...", false}
	close(outputCh)
	wg.Wait()
}

func crawl(crawler *crawler, depth int, server *headless.Server) {
	defer func() {
		if atomic.AddInt64(&counter, -1) == 0 && len(crawler.urlTopoCh) == 0 {
			close(crawler.urlTopoCh)
		}
	}()

	dir := filepath.Join(crawler.config.prefix, crawler.rootURL.Hostname())
	rooturl := crawler.rootURL.String()
	if server != nil {
		resp, err := http.Head(rooturl)
		if err != nil {
			outputCh <- &output{err, false}
			return
		}
		defer resp.Body.Close()

		MIME, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if MIME == "text/html" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			HTMLContent, err := server.GetWebpageSourceCode(ctx, rooturl)
			if err != nil {
				outputCh <- &output{err, false}
				return
			}
			if err = parseHTML(strings.NewReader(HTMLContent), crawler, dir); err != nil {
				outputCh <- &output{err, false}
				return
			}
		}
	}
	req, err := http.NewRequest(http.MethodGet, rooturl, nil)
	if err != nil {
		outputCh <- &output{err, false}
		return
	}
	req.Header.Set("Accept-Charset", "utf-8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", userAgent)

	resp, err := crawler.client.Do(req)
	if err != nil {
		outputCh <- &output{err, false}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		outputCh <- &output{fmt.Sprintf("%q: %s", rooturl, http.StatusText(resp.StatusCode)), false}
		return
	}
	if !resp.Uncompressed {
		resp.Body, err = uncompressBody(resp)
	}

	MIME, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		outputCh <- &output{err, false}
		return
	}

	switch MIME {
	case "application/octet-stream":
		if err := handleOctetStream(resp, dir, crawler.config.fileTypes); err != nil {
			outputCh <- &output{err, false}
		}
	case "text/html":
		rd, err := convertToUnescapedUTF8Body(resp, params)
		if err != nil {
			outputCh <- &output{err, false}
			return
		}
		if err = parseHTML(rd, crawler, dir); err != nil {
			outputCh <- &output{err, false}
		}
	default:
		if ext, ok := matchExtsWithMIME(MIME, crawler.config.fileTypes); ok {
			outputCh <- &output{fmt.Sprintf("Found %s file (%s)", ext[1:], rooturl), false}
			if err := writeFile(resp.Body, dir, getFileName(crawler.rootURL.Path, ext)); err != nil {
				outputCh <- &output{err, false}
			}
		}
	}
}

func handleOctetStream(resp *http.Response, dir string, ftypes []string) error {
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err == nil {
		if v, ok := params["filename"]; ok && v != "" {
			if ext, _, ok := containsAnyExts(v, ftypes); ok {
				outputCh <- &output{fmt.Sprintf("Found %s file (%s)", ext[1:], params["filename"]), false}
				return writeFile(resp.Body, dir, params["filename"])
			}
		}
	}
	if resp.Request != nil {
		path := resp.Request.URL.Path
		if ext, _, ok := containsAnyExts(path, ftypes); ok {
			return writeFile(resp.Body, dir, getFileName(path, ext))
		}
	}
	return nil
}

func convertToUnescapedUTF8Body(resp *http.Response, params map[string]string) (io.Reader, error) {
	enc, err := detectCharset(resp.Body, params)
	if err != nil {
		return nil, err
	}
	byts, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(enc, unicode.UTF8) {
		if byts, err = enc.NewDecoder().Bytes(byts); err != nil {
			return nil, err
		}
	}
	return strings.NewReader(html.UnescapeString(string(byts))), nil
}

func detectCharset(r io.Reader, params map[string]string) (encoding.Encoding, error) {
	if encName := params["charset"]; encName != "" {
		enc, err := htmlindex.Get(encName)
		if err == nil {
			return enc, nil
		}
	}
	buf := bufio.NewReader(r)
	byts, err := buf.Peek(1024)
	if err == nil {
		if enc, _, ok := charset.DetermineEncoding(byts, ""); ok {
			return enc, nil
		}
	}
	return nil, errors.New("Failed to detect charset")
}

func parseHTML(rd io.Reader, crawler *crawler, dir string) error {
	var buf *bytes.Buffer
	var data []byte
	var err error
	if crawler.downloadHTML {
		buf = new(bytes.Buffer)
		tee := io.TeeReader(rd, buf)
		data, err = ioutil.ReadAll(tee)
		if err != nil {
			return err
		}
		rd = buf
	}

	var depth int
	tz := html.NewTokenizer(rd)
	for {
		tt := tz.Next()
		if tz.Err() == io.EOF {
			break
		}
		switch tt {
		case html.ErrorToken:
			outputCh <- &output{tz.Err(), false}
		case html.TextToken:
			if crawler.downloadHTML && depth > 0 {
				if err = writeFile(bytes.NewReader(data), dir, string(tz.Text())+".html"); err != nil {
					outputCh <- &output{err, false}
				}
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tz.TagName()
			if string(tn) == "title" && crawler.downloadHTML {
				if tt == html.StartTagToken {
					depth++
				} else if tt == html.EndTagToken {
					depth--
				}
				continue
			}
			if hasAttr {
				for {
					key, val, more := tz.TagAttr()
					if string(tn) == "a" && string(key) == "href" && (depth > 1 || depth == -1) {
						if strings.HasPrefix(string(val), "javascript:") {
							break
						}
						actualURL, err := fixedURL(crawler.rootURL, string(val))
						if err != nil {
							outputCh <- &output{err, false}
							break
						}
						if !crawler.config.opt.externalWebpages && actualURL.Hostname() != crawler.rootURL.Hostname() {
							break
						}
						if actualURL.Fragment != "" {
							actualURL.Fragment = ""
						}
						actualurl := actualURL.String()
						if _, loaded := crawledURL.LoadOrStore(actualurl, true); !loaded {
							outputCh <- &output{fmt.Sprintf("Found new url %q on %s", actualurl, crawler.rootURL.String()), false}
							if depth != -1 {
								crawler.urlTopoCh <- &urlTopological{actualURL, depth - 1}
							} else {
								crawler.urlTopoCh <- &urlTopological{actualURL, -1}
							}
						}
						break
					} else if string(key) == "src" || (string(tn) == "link" && string(key) == "href") {
						if strings.HasPrefix(string(val), "data:") {
							if err := writeBase64ImageFile(string(val), dir); err != nil {
								outputCh <- &output{err, false}
							}
							break
						}
						ext, idx, ok := containsAnyExts(string(val), crawler.config.fileTypes)
						if !ok {
							break
						}
						refurl := string(val[:idx+len(ext)])
						actualURL, err := fixedURL(crawler.rootURL, refurl)
						if err != nil {
							outputCh <- &output{err, false}
							break
						}
						if actualURL.Fragment != "" {
							actualURL.Fragment = ""
						}
						if _, loaded := crawledURL.LoadOrStore(actualURL.String(), true); loaded {
							break
						}

						req, err := http.NewRequest(http.MethodGet, actualURL.String(), nil)
						if err != nil {
							outputCh <- &output{err, false}
							break
						}
						req.Header.Set("Accept-Charset", "utf-8")
						req.Header.Set("Accept-Encoding", "gzip, deflate")
						req.Header.Set("User-Agent", userAgent)
						resp, err := crawler.client.Do(req)
						if err != nil {
							outputCh <- &output{err, false}
							break
						}
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							outputCh <- &output{fmt.Sprintf("%q: %s", actualURL.String(), http.StatusText(resp.StatusCode)), false}
							break
						}

						MIME, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
						outputCh <- &output{fmt.Sprintf("Found %s file (%s) on %s", ext[1:], actualURL.String(), crawler.rootURL.String()), false}
						if MIME != mime.TypeByExtension(ext) {
							outputCh <- &output{fmt.Sprintf("%q: MIME type in Content-Type mismatch file extension name", actualURL.String()), false}
						}
						if err = writeFile(resp.Body, dir, getFileName(actualURL.Path, ext)); err != nil {
							outputCh <- &output{err, false}
						}
						break
					}
					if !more {
						break
					}
				}
			}
		}
	}
	return nil
}

func getFileName(path, ext string) string {
	var fileName string
	base := filepath.Base(path)
	if base != "." && base != string(filepath.Separator) && base != string(filepath.ListSeparator) {
		if idx := strings.Index(base, ext); idx != -1 {
			fileName = base[:idx+len(ext)]
		} else {
			fileName = base + ext
		}
	} else {
		fileName = randomString(16) + ext
	}
	return fileName
}

func matchExtsWithMIME(MIME string, ftypes []string) (string, bool) {
	exts, err := mime.ExtensionsByType(MIME)
	if err != nil {
		return "", false
	}
	for _, ext := range exts {
		if _, _, ok := containsAnyExts(ext, ftypes); ok {
			return ext, true
		}
	}
	return "", false
}

func writeFile(rd io.Reader, dir, filename string) error {
	_, err := os.Stat(dir)
	if err != nil && os.IsNotExist(err) {
		if err = os.MkdirAll(dir, 01775); err != nil {
			return err
		}
	}
	data, err := ioutil.ReadAll(rd)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join(dir, filename), data, 0755)
}

func writeBase64ImageFile(uri, dir string) error {
	colonIdx := strings.IndexByte(uri, ':')
	semicolonIdx := strings.LastIndexByte(uri, ';')
	if colonIdx != -1 && semicolonIdx != -1 {
		exts, err := mime.ExtensionsByType(uri[colonIdx+1 : semicolonIdx])
		if err == nil {
			commaIdx := strings.IndexByte(uri, ',')
			if commaIdx != -1 {
				rd := base64.NewDecoder(base64.StdEncoding, strings.NewReader(uri[commaIdx+1:]))
				for _, ext := range exts {
					switch ext[1:] {
					case "png":
						img, err := png.Decode(rd)
						if err != nil {
							return err
						}
						f, err := os.Create(filepath.Join(dir, randomString(16)+ext))
						if err != nil {
							return err
						}
						return png.Encode(f, img)
					case "jpg", "jpeg":
						img, err := jpeg.Decode(rd)
						if err != nil {
							return err
						}
						f, err := os.Create(filepath.Join(dir, randomString(16)+ext))
						if err != nil {
							return err
						}
						return png.Encode(f, img)
					case "gif":
						GIF, err := gif.DecodeAll(rd)
						if err != nil {
							return err
						}
						f, err := os.Create(filepath.Join(dir, randomString(16)+ext))
						if err != nil {
							return err
						}
						return gif.EncodeAll(f, GIF)
					}
				}
				return errors.New("Unsupported base64 URI media type")
			}
		}
	}
	return errors.New("Invalid base64 URI format")
}

func randomString(n int) string {
	randomBytes := make([]byte, n)
	rand.Read(randomBytes)
	ret := make([]byte, n)
	for i := range randomBytes {
		idx := randomBytes[i] % uint8(len(characterSet))
		ret[i] = characterSet[idx]
	}
	return string(ret)
}

func uncompressBody(resp *http.Response) (io.ReadCloser, error) {
	enc := resp.Header.Get("Content-Encoding")
	switch enc {
	case "gzip":
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		return zr, nil
	case "deflate":
		dfr := flate.NewReader(resp.Body)
		return dfr, nil
	default:
		return nil, errors.New("Unknown Content-Encoding: " + enc)
	}
}

func containsAnyExts(str string, exts []string) (string, int, bool) {
	dotIdx := strings.LastIndexByte(str, '.')
	if dotIdx != -1 {
		for _, ext := range exts {
			if str[dotIdx:] == ext || strings.HasPrefix(str[dotIdx:], ext) ||
				str[dotIdx+1:] == ext || strings.HasPrefix(str[dotIdx+1:], ext) {
				if ext[0] == '.' {
					return ext, dotIdx, true
				}
				return "." + ext, dotIdx, true
			}
		}
	}
	return "", dotIdx, false
}

func fixedURL(baseURL *url.URL, ref string) (*url.URL, error) {
	refURL, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	return baseURL.ResolveReference(refURL), nil
}

func setProxy(transport *http.Transport, p *proxy) error {
	if p == nil {
		transport.Proxy = http.ProxyFromEnvironment
		return nil
	}

	u := p.URL()
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "socks5" {
		transport.Proxy = http.ProxyURL(u)
		return nil
	}
	return errors.New(unsupportProxy)
}
