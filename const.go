package main

const (
	versionInfo    = "v0.9.0 beta"
	proxyUsage     = "Specify proxy address, currenctly support http(s) and socks5 proxy only"
	configUsage    = "Path to the configuration file"
	depthUsage     = "The depth of crawling webpage. 0 means that doesn't crawl any source, 1 indicates that crawling the url passed as command-line arg only, -1 means no limit (default 0)"
	maxConUsage    = "The maximum number of concurrency that allowed. 0 means no limit (default 1)"
	listUsage      = "Comma-separated without white space added list of file type to grab, e.g., jpeg,mp4,jpg,png"
	externalUsage  = "Enable crawling external webpage (default false)"
	versionUsage   = "Display version info"
	dirUsage       = "Specify the directory where the file is saved"
	headlessUsage  = "Enable headless mode, require Chrome browser 59+ to be installed on your system"
	logUsage       = "Path to the log file"
	cookieUsage    = "Disable Cookie support"
	emptyListSet   = "File types can't be empty set! please use -ftypes to set or specify in configuration file and try again"
	invalidDepth   = "Invalid value, please provide an integer value greater than -2 and try again"
	invalidMaxCon  = "Invalid value, please provide an integer value greater than or equal to 0 and try again"
	invalidftypes  = "Invalid value, please provide a non-empty comma-separated without white space added list of file types and try again"
	invalidURL     = "Invalid HTTP/HTTPS URL"
	unsupportProxy = "Unsupported proxy type, currenctly support http(s) and socks5 proxy only"
	notice         = "If flag valus provided, the corresponding settings in config file will be override"
)
