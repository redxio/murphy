package crawl

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/unicode"
)

func (crawler *Crawler) parseHTML(rd io.Reader, urlTopo *URLTopological, dir string) {
	var (
		data []byte
		err  error
	)

	if crawler.DownloadHTML {
		buf := new(bytes.Buffer)
		tee := io.TeeReader(rd, buf)

		data, err = ioutil.ReadAll(tee)
		if err != nil {
			crawler.Logger.Printf("Reading error while parsing HTML: %v", err)
			return
		}

		rd = buf
	}

	var (
		depth int
		tt html.TokenType
		tz = html.NewTokenizer(rd)
	)

	for {
		tt = tz.Next()
		if tz.Err() == io.EOF {
			break
		}
		switch tt {
		case html.ErrorToken:
			crawler.Logger.Printf("Error token while parsing HTML: %v", tz.Err())
		case html.TextToken:
			if crawler.DownloadHTML && depth > 0 {
				if err = writeFile(bytes.NewReader(data), dir, string(tz.Text())+".html"); err != nil {
					crawler.Logger.Printf("Error writing file %s: %v", string(tz.Text())+".html", err)
				}
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tz.TagName()
			if string(tn) == "title" && crawler.DownloadHTML {
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

					if string(tn) == "a" && string(key) == "href" && (urlTopo.Depth > 1 || urlTopo.Depth == -1) {
						if strings.HasPrefix(string(val), "javascript:") {
							break
						}

						actualURL, err := fixedURL(urlTopo.URL, string(val))
						if err != nil {
							crawler.Logger.Printf("Error resolving ref url: %v\n", err)
							break
						}

						if !crawler.config.Option.ExternalWebpages && actualURL.Hostname() != urlTopo.URL.Hostname() {
							break
						}

						if actualURL.Fragment != "" {
							actualURL.Fragment = ""
						}

						actualurl := actualURL.String()
						if _, loaded := crawler.crawledURL.LoadOrStore(actualurl, true); !loaded {
							crawler.Logger.Printf("Found new url %q on %s\n", actualurl, urlTopo.URL.String())
							if urlTopo.Depth != -1 {
								crawler.URLTopoCh <- &URLTopological{actualURL, urlTopo.Depth - 1}
							} else {
								crawler.URLTopoCh <- &URLTopological{actualURL, -1}
							}
						}
						break
					} else if string(key) == "src" || (string(tn) == "link" && string(key) == "href") {
						if strings.HasPrefix(string(val), "data:") {
							if err := writeBase64ImageFile(string(val), crawler.config.Dir); err != nil {
								crawler.Logger.Printf("Error writing base64 image file: %v\n", err)
							}
							break
						}

						ext, idx, ok := containsAnyExts(string(val), crawler.config.FileTypes)
						if !ok {
							break
						}

						refurl := string(val[:idx+len(ext)])
						actualURL, err := fixedURL(urlTopo.URL, refurl)
						if err != nil {
							crawler.Logger.Printf("Error resolving ref url: %v\n", err)
							break
						}

						if actualURL.Fragment != "" {
							actualURL.Fragment = ""
						}

						if _, loaded := crawler.crawledURL.LoadOrStore(actualURL.String(), true); loaded {
							break
						}

						req, err := http.NewRequest(http.MethodGet, actualURL.String(), nil)
						if err != nil {
							crawler.Logger.Printf("Error creating HTTP request: %v\n", err)
							break
						}

						req.Header.Set("Accept-Charset", "utf-8")
						req.Header.Set("Accept-Encoding", "gzip, deflate")
						req.Header.Set("User-Agent", crawler.config.UserAgent)

						resp, err := crawler.client.Do(req)
						if err != nil {
							crawler.Logger.Printf("HTTP Error: %v\n", err)
							break
						}
						defer resp.Body.Close()

						if resp.StatusCode != http.StatusOK {
							crawler.Logger.Printf("URL: %s, status text: %v\n", actualURL.String(), http.StatusText(resp.StatusCode))
							break
						}

						MIME, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
						crawler.Logger.Printf("Found file %s on %v", getFileName(actualURL.Path, ext), actualURL.String())
						if MIME != mime.TypeByExtension(ext) {
							crawler.Logger.Printf("URL: %s, MIME type in Content-Type mismatch file extension name", actualURL.String())
						}

						if err = writeFile(resp.Body, dir, getFileName(actualURL.Path, ext)); err != nil {
							crawler.Logger.Printf("Error writing file %s: %v", string(tz.Text())+".html", err)
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
		return nil, errors.New("unknown Content-Encoding: " + enc)
	}
}

func (crawler *Crawler) handleOctetStream(resp *http.Response, dir string) {
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err == nil {
		if v, ok := params["filename"]; ok && v != "" {
			if _, _, ok := containsAnyExts(v, crawler.config.FileTypes); ok {
				crawler.Logger.Printf("Found file %s on %v", params["filename"], resp.Request.URL.String())
				if err = writeFile(resp.Body, dir, params["filename"]); err != nil {
					crawler.Logger.Printf("Error writing file %s: %v", params["filename"], err)
				}
				return
			}
		}
	}
	if resp.Request != nil {
		path := resp.Request.URL.Path
		if ext, _, ok := containsAnyExts(path, crawler.config.FileTypes); ok {
			if err = writeFile(resp.Body, dir, getFileName(path, ext)); err != nil {
				crawler.Logger.Printf("Error writing file %s: %v", params["filename"], err)
			}
		}
	}
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

func MatchMIMEInExts(MIME string, ftypes []string) (string, bool) {
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

func fixedURL(baseURL *url.URL, ref string) (*url.URL, error) {
	refURL, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	return baseURL.ResolveReference(refURL), nil
}
