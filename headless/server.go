package headless

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/mafredri/cdp"
	"github.com/mafredri/cdp/protocol/dom"
	"github.com/mafredri/cdp/protocol/page"
	"github.com/mafredri/cdp/rpcc"

	"github.com/mafredri/cdp/devtool"
)

// Server is used for interacting with headless Chrome browser
type Server struct {
	cmd      *exec.Cmd
	port     int
	devtools *devtool.DevTools
	args     []string
}

// New returns a new Server instance
func New() *Server {
	return &Server{
		args: []string{
			"--headless",
			"--disable-gpu",
			"--no-sandbox",
		},
	}
}

func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// Start starts headless Chrome browser
func (ser *Server) Start() error {
	if ser == nil {
		return errors.New("The server instance is not created")
	}
	if ser.cmd != nil {
		return errors.New("The server is already running")
	}
	var executablePath string
label:
	switch runtime.GOOS {
	case "linux", "freebsd":
		available := []string{"google-chrome-stable", "google-chrome", "headless_shell", "chromium-browser", "chromium"}
		for _, v := range available {
			path, err := exec.LookPath(v)
			if err == nil {
				executablePath = path
				break label
			}
		}
		return errors.New("Can't find Chrome on your Linux system")
	case "windows":
		path, err := exec.LookPath("chrome.exe")
		if err == nil {
			executablePath = path
			break
		}
		path, err = exec.LookPath(`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`)
		if err == nil {
			executablePath = path
			break
		}
		return errors.New("Can't find Chrome on your Windows system")
	case "darwin":
		path, err := exec.LookPath(`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`)
		if err == nil {
			executablePath = path
			break label
		}
		return errors.New("Can't find Chrome on your MacOS system")
	}

	if port, err := getFreePort(); err == nil {
		ser.port = port
	} else {
		return err
	}

	ser.args = append(ser.args, fmt.Sprintf("--remote-debugging-port=%d", ser.port))
	ser.cmd = exec.Command(executablePath, ser.args...)
	ser.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := ser.cmd.Start(); err != nil {
		return err
	}
	ser.devtools = devtool.New(fmt.Sprintf("http://127.0.0.1:%d", ser.port))
	return nil
}

// GetWebpageSourceCode get html source code for webpage, including javascript-based dynamic webpage
func (ser *Server) GetWebpageSourceCode(ctx context.Context, url string) (string, error) {
	if ser == nil {
		return "", errors.New("The server instance is not created")
	}
	if ser.cmd == nil {
		return "", errors.New("The headless Chrome is not running")
	}
	var target *devtool.Target
	var err error
loop:
	for {
		select {
		case <-ctx.Done():
			return "", nil
		default:
			target, err = ser.devtools.Get(ctx, devtool.Page)
			if err == nil {
				break loop
			}
			continue
		}
	}
	conn, err := rpcc.DialContext(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	client := cdp.NewClient(conn)
	domLoadFired, err := client.Page.DOMContentEventFired(ctx)
	if err != nil {
		return "", err
	}
	defer domLoadFired.Close()
	if err = client.Page.Enable(ctx); err != nil {
		return "", err
	}

	navArgs := page.NewNavigateArgs(url)
	if _, err = client.Page.Navigate(ctx, navArgs); err != nil {
		return "", err
	}
	if _, err = domLoadFired.Recv(); err != nil {
		return "", err
	}
	doc, err := client.DOM.GetDocument(ctx, nil)
	if err != nil {
		return "", err
	}
	result, err := client.DOM.GetOuterHTML(ctx, &dom.GetOuterHTMLArgs{
		NodeID: &doc.Root.NodeID,
	})
	if err != nil {
		return "", err
	}
	return result.OuterHTML, nil
}

// Stop kills spawned background child process
func (ser *Server) Stop() error {
	if ser == nil {
		return errors.New("The server instance is not created")
	}
	if ser.cmd == nil {
		return errors.New("The headless Chrome is not running")
	}
	return syscall.Kill(-ser.cmd.Process.Pid, syscall.SIGKILL)
}
