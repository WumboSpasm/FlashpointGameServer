package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/krum110487/zipfs"
)

type ServerSettings struct {
	AllowCrossDomain     bool              `json:"allowCrossDomain"`
	VerboseLogging       bool              `json:"verboseLogging"`
	ProxyPort            string            `json:"proxyPort"`
	ServerHTTPPort       string            `json:"serverHTTPPort"`
	GameRootPath         string            `json:"gameRootPath"`
	ApiPrefix            string            `json:"apiPrefix"`
	ExternalFilePaths    []string          `json:"externalFilePaths"`
	ExtScriptTypes       []string          `json:"extScriptTypes"`
	ExtIndexTypes        []string          `json:"extIndexTypes"`
	ExtMimeTypes         map[string]string `json:"extMimeTypes"`
	ExtGzippeddTypes     []string          `json:"extGzippedTypes"`
	UseMad4FP            bool              `json:"useMad4FP"`
	HandleLegacyRequests bool              `json:"handleLegacyRequests"`
	ExternalLegacyPort   string            `json:"externalLegacyPort"`
	LegacyHTDOCSPath     string            `json:"legacyHTDOCSPath"`
	RootPath             string            `json:"rootPath"`
	LegacyCGIBINPath     string            `json:"legacyCGIBINPath"`
	PhpCgiPath           string            `json:"phpCgiPath"`
	OverridePaths        []string          `json:"overridePaths"`
	LegacyOverridePaths  []string          `json:"legacyOverridePaths"`
	UseInfinityServer    bool              `json:"useInfinityServer"`
	InfinityServerURL    string            `json:"infinityServerURL"`
}

// ExtApplicationTypes is a map that holds the content types of different file extensions
var serverSettings ServerSettings
var proxy *goproxy.ProxyHttpServer
var cwd string

func initServer() {
	// Load the content types from the JSON file
	data, err := os.ReadFile("proxySettings.json")
	if err != nil {
		panic(err)
	}

	// Unmarshal the JSON data into a Config struct
	err = json.Unmarshal(data, &serverSettings)
	if err != nil {
		panic(err)
	}

	//Get the CWD of this application
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	cwd = strings.ReplaceAll(filepath.Dir(exe), "\\", "/")

	//TODO: Update proxySettings.LegacyHTDOCSPath AND proxySettings.LegacyPHPPath for the default values!

	//Get all of the paramaters passed in.
	verboseLogging := flag.Bool("v", false, "should every proxy request be logged to stdout")
	proxyPort := flag.Int("proxyPort", 22500, "proxy listen port")
	serverHTTPPort := flag.Int("serverHttpPort", 22501, "zip server http listen port")
	gameRootPath := flag.String("gameRootPath", "D:\\Flashpoint\\Data\\Games", "This is the path where to find the zips")
	rootPath := flag.String("rootPath", "D:\\Flashpoint", "The path that other relative paths use as a base")
	apiPrefix := flag.String("apiPrefix", "/fpProxy/api", "apiPrefix is used to prefix any API call.")
	useMad4FP := flag.Bool("UseMad4FP", false, "flag to turn on/off Mad4FP.")
	externalLegacyPort := flag.String("externalLegacyPort", "22600", "The port that the external legacy server is running on (if handling legacy is disabled).")
	legacyHTDOCSPath := flag.String("legacyHTDOCSPath", "D:\\Flashpoint\\Legacy\\htdocs", "This is the path for HTDOCS")
	phpCgiPath := flag.String("phpCgiPath", "D:\\Flashpoint\\Legacy\\php-cgi.exe", "Path to PHP CGI executable")
	useInfinityServer := flag.Bool("useInfinityServer", false, "Whether to use the infinity server or not")
	infinityServerURL := flag.String("infinityServerURL", "https://example.com/", "The URL of the infinity server")

	flag.Parse()

	//Apply all of the flags to the settings
	serverSettings.VerboseLogging = *verboseLogging
	serverSettings.RootPath, err = filepath.Abs(strings.Trim(*rootPath, "\""))
	if err != nil {
		fmt.Printf("Failed to get absolute game root path")
		return
	}
	serverSettings.ProxyPort = strconv.Itoa(*proxyPort)
	serverSettings.ServerHTTPPort = strconv.Itoa(*serverHTTPPort)
	serverSettings.ApiPrefix = *apiPrefix
	serverSettings.UseMad4FP = *useMad4FP
	serverSettings.ExternalLegacyPort = *externalLegacyPort
	serverSettings.LegacyHTDOCSPath, err = filepath.Abs(path.Join(serverSettings.RootPath, strings.Trim(*legacyHTDOCSPath, "\"")))
	if err != nil {
		fmt.Printf("Failed to get absolute htdocs path")
		return
	}
	serverSettings.GameRootPath, err = filepath.Abs(path.Join(serverSettings.RootPath, strings.Trim(*gameRootPath, "\"")))
	if err != nil {
		fmt.Printf("Failed to get absolute game root path")
		return
	}
	serverSettings.PhpCgiPath, err = filepath.Abs(path.Join(serverSettings.RootPath, strings.Trim(*phpCgiPath, "\"")))
	if err != nil {
		fmt.Printf("Failed to get absolute php cgi path")
		return
	}
	serverSettings.UseInfinityServer = *useInfinityServer
	serverSettings.InfinityServerURL = *infinityServerURL

	// Print out all path settings
	fmt.Printf("Root Path: %s\n", serverSettings.RootPath)
	fmt.Printf("PHP CGI Path: %s\n", serverSettings.PhpCgiPath)
	fmt.Printf("Legacy HTDOCS Path: %s\n", serverSettings.LegacyHTDOCSPath)

	//Setup the proxy.
	proxy = goproxy.NewProxyHttpServer()
	proxy.Verbose = serverSettings.VerboseLogging
	fmt.Printf("Proxy Server Started on port %s\n", serverSettings.ProxyPort)
	fmt.Printf("Zip Server Started\n\tHTTP Port: %s\n\tGame Root: %s\n",
		serverSettings.ServerHTTPPort,
		serverSettings.GameRootPath)
}

func setContentType(r *http.Request, resp *http.Response) {
	if r == nil || resp == nil {
		return
	}

	rext := strings.ToLower(filepath.Ext(resp.Header.Get("ZIPSVR_FILENAME")))
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(r.URL.Path)))
	mime := ""

	// If the request already has an extension, fetch the mime via extension
	if ext != "" {
		resp.Header.Set("Content-Type", serverSettings.ExtMimeTypes[ext[1:]])
		mime = serverSettings.ExtMimeTypes[ext[1:]]
		if mime != "" && len(ext) > 1 {
			resp.Header.Set("Content-Type", mime)
			e := ext[1:]
			// If pre-compressed set encoding type
			for _, element := range serverSettings.ExtGzippeddTypes {
				if element == e {
					resp.Header.Set("Content-Encoding", "gzip")
					break // String found, no need to continue iterating
				}
			}
		}
	}

	// If the response has an extension, try and fetch the mime for that via extension
	if mime == "" && rext != "" {
		resp.Header.Set("Content-Type", serverSettings.ExtMimeTypes[rext[1:]])
		mime = serverSettings.ExtMimeTypes[rext[1:]]
		if mime != "" && len(rext) > 1 {
			resp.Header.Set("Content-Type", mime)
			e := rext[1:]
			// If pre-compressed set encoding type
			for _, element := range serverSettings.ExtGzippeddTypes {
				if element == e {
					resp.Header.Set("Content-Encoding", "gzip")
					break // String found, no need to continue iterating
				}
			}
		}
	}

	// Set content type header
	if mime != "" {
		resp.Header.Set("Content-Type", mime)
	}
}

func handleRequest(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	// Remove port from host if exists (old apps don't clean it before sending requests?)
	r.URL.Host = strings.Split(r.URL.Host, ":")[0]

	// Copy the original request
	gamezipRequest := &http.Request{
		Method: r.Method,
		URL: &url.URL{
			Scheme:   "http",
			Host:     "127.0.0.1:" + serverSettings.ServerHTTPPort,
			Path:     "content/" + r.URL.Host + r.URL.Path,
			RawQuery: r.URL.RawQuery,
		},
		Header: r.Header,
		Body:   r.Body,
	}

	// Clone the body into both requests by reading and making 2 new readers
	contents, _ := ioutil.ReadAll(r.Body)
	gamezipRequest.Body = ioutil.NopCloser(bytes.NewReader(contents))

	// Make the request to the zip server.
	client := &http.Client{}
	proxyReq, err := http.NewRequest(gamezipRequest.Method, gamezipRequest.URL.String(), gamezipRequest.Body)
	if err != nil {
		fmt.Printf("UNHANDLED GAMEZIP ERROR: %s\n", err)
	}
	proxyReq.Header = gamezipRequest.Header
	proxyResp, err := client.Do(proxyReq)

	if proxyResp.StatusCode >= 500 {
		fmt.Println("Gamezip Server Error: ", proxyResp.StatusCode)
	}

	// Check Legacy
	if proxyResp.StatusCode >= 400 {
		// Copy the original request
		legacyRequest := &http.Request{
			Method: r.Method,
			URL: &url.URL{
				Scheme:   "http",
				Host:     r.URL.Host,
				Path:     r.URL.Path,
				RawQuery: r.URL.RawQuery,
			},
			Header: r.Header,
			Body:   r.Body,
		}
		// Copy in a new body reader
		legacyRequest.Body = ioutil.NopCloser(bytes.NewReader(contents))

		// Choose which legacy method we're using
		if serverSettings.HandleLegacyRequests {
			// If internal, skip actual networking
			resRecorder := httptest.NewRecorder()
			ServeLegacy(resRecorder, legacyRequest)
			proxyResp = resRecorder.Result()
		} else {
			// Set the Proxy URL and apply it to the Transpor layer so that the request respects the proxy.
			proxyURL, _ := url.Parse("http://127.0.0.1:" + serverSettings.ExternalLegacyPort)
			proxy := http.ProxyURL(proxyURL)
			transport := &http.Transport{Proxy: proxy}

			// A custom Dialer is required for the "localflash" urls, instead of using the DNS, we use this.
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				//Set Dialer timeout and keepalive to 30 seconds and force the address to localhost.
				dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
				addr = "127.0.0.1:" + serverSettings.ExternalLegacyPort
				return dialer.DialContext(ctx, network, addr)
			}

			// Make the request with the custom transport
			client := &http.Client{Transport: transport, Timeout: 300 * time.Second}
			legacyResp, err := client.Do(legacyRequest)
			// An error occured, log it for debug purposes
			if err == nil {
				fmt.Printf("\tServing from External Legacy...\n")
				proxyResp = legacyResp
			} else {
				fmt.Printf("UNHANDLED EXTERNAL LEGACY ERROR: %s\n", err)
			}
		}
	}

	// Update the content type based upon ext for now.
	setContentType(r, proxyResp)

	// Add extra headers
	proxyResp.Header.Set("Access-Control-Allow-Origin", "*")
	// Keep Alive
	if strings.ToLower(r.Header.Get("Connection")) == "keep-alive" {
		proxyResp.Header.Set("Connection", "Keep-Alive")
		proxyResp.Header.Set("Keep-Alive", "timeout=5; max=100")
	}

	return r, proxyResp
}

func main() {
	initServer()
	// To create CA cert, refer to https://wiki.mozilla.org/SecurityEngineering/x509Certs#Self_Signed_Certs
	// Replace CA in GoProxy
	certData := []byte(`-----BEGIN CERTIFICATE-----
MIICJDCCAcsCFFCWJV/hBHpY18k/14yUbDA6V/TTMAoGCCqGSM49BAMCMIGTMQsw
CQYDVQQGEwJVUzETMBEGA1UECAwKU29tZS1TdGF0ZTEoMCYGA1UECgwfRmxhc2hw
b2ludCBQcm94eSBVbnRydXN0ZWQgTUlUTTEoMCYGA1UECwwfRmxhc2hwb2ludCBQ
cm94eSBVbnRydXN0ZWQgTUlUTTEbMBkGA1UEAwwSZnBwcm94eS5sb2NhbC5zaXRl
MCAXDTIzMTAxNDEzNTQxNVoYDzIxMjMwOTIwMTM1NDE1WjCBkzELMAkGA1UEBhMC
VVMxEzARBgNVBAgMClNvbWUtU3RhdGUxKDAmBgNVBAoMH0ZsYXNocG9pbnQgUHJv
eHkgVW50cnVzdGVkIE1JVE0xKDAmBgNVBAsMH0ZsYXNocG9pbnQgUHJveHkgVW50
cnVzdGVkIE1JVE0xGzAZBgNVBAMMEmZwcHJveHkubG9jYWwuc2l0ZTBZMBMGByqG
SM49AgEGCCqGSM49AwEHA0IABDOkMb4Fb+waYfEXg5OszAyjNqcp8PLTqSC2fcfC
gX3Wqgvq4Vf46F4FViDKyo+E+6fOm3MauI3Vg2FGKUXf9jowCgYIKoZIzj0EAwID
RwAwRAIgHyjrkkCwuOQm5JO5SKeH3Om8dQm6m6a+1k5max2RqakCICQRzrm0ERo4
siAXSthMrOdDignP/cM10AcBe/J00Vw8
-----END CERTIFICATE-----`)
	keyData := []byte(`-----BEGIN EC PARAMETERS-----
BggqhkjOPQMBBw==
-----END EC PARAMETERS-----
-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIGfj1mtowe1WiAMA3mK1VjgXV1lgUkliUxnk6lr5y/g5oAoGCCqGSM49
AwEHoUQDQgAEM6QxvgVv7Bph8ReDk6zMDKM2pynw8tOpILZ9x8KBfdaqC+rhV/jo
XgVWIMrKj4T7p86bcxq4jdWDYUYpRd/2Og==
-----END EC PRIVATE KEY-----`)

	cert, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		panic(err)
	}

	goproxy.MitmConnect.TLSConfig = goproxy.TLSConfigFromCA(&cert)

	// Handle HTTPS requests (DOES NOT HANDLE HTTP)
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.OnRequest().HijackConnect(func(req *http.Request, client net.Conn, ctx *goproxy.ProxyCtx) {
		_, resp := handleRequest(req, ctx)
		resp.Write(client)
		client.Close()
	})

	// Handle HTTP requests (DOES NOT HANDLE HTTPS)
	proxy.OnRequest().DoFunc(func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		return handleRequest(r, ctx)
	})

	// Start ZIP server
	go func() {
		//TODO: Update these to be modifiable in the properties json.
		//TODO: Also update the "fpProxy/api/" to be in the properties json.
		log.Fatal(http.ListenAndServe("127.0.0.1:"+serverSettings.ServerHTTPPort,
			zipfs.EmptyFileServer(
				serverSettings.ApiPrefix,
				"",
				serverSettings.VerboseLogging,
				serverSettings.ExtIndexTypes,
				serverSettings.GameRootPath,
				serverSettings.PhpCgiPath,
				serverSettings.ExtMimeTypes,
				serverSettings.OverridePaths,
				serverSettings.LegacyHTDOCSPath,
			),
		))
	}()

	// Start proxy server
	log.Fatal(http.ListenAndServe("127.0.0.1:"+serverSettings.ProxyPort, http.AllowQuerySemicolons(proxy)))
}
