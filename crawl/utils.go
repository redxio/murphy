package crawl

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const characterSet = "_-9876543210ABCDEFGHIJKMNLOPQRSTUVWXYZzyxwvutsrqpolnmkjihgfedcba"

func writeFile(rd io.Reader, dir, filename string) error {
	data, err := ioutil.ReadAll(rd)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join(dir, filename), data, 0755)
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
				return errors.New("unsupported base64 URI media type")
			}
		}
	}
	return errors.New("invalid base64 URI format")
}

func createRequest(method, url, userAgent string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept-Charset", "utf-8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", userAgent)

	return req, nil
}
