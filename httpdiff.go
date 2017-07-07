// httpdiff: performs two HTTP requests and diffs the responses
//
// Copyright (c) 2015 John Graham-Cumming
package httpautodiff

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/sergi/go-diff/diffmatchpatch"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode"
)

// Set to true to prevent colour output
var mono = false

var notsame = false

var transport = &http.Transport{}

// ANSI escape functions and print helpers
func on(i int, s string) string {
	if mono {
		return fmt.Sprintf("%d: %s", i+1, s)
	}
	return fmt.Sprintf("\x1b[3%dm%s\x1b[0m", i*3+1, s)
}
func oni(i, d int) string {
	return on(i, fmt.Sprintf("%d", d))
}
func green(s string) string {
	if mono {
		return fmt.Sprintf("'%s'", s)
	}
	return fmt.Sprintf("\x1b[32m%s\x1b[0m", s)
}
func vs(a, b string, f string, v ...interface{}) bool {
	notsame = a != b
	if notsame {
		s := fmt.Sprintf(f, v...)
		fmt.Printf("%s\n    %s\n    %s\n", s, on(0, a), on(1, b))
	}
	return notsame
}
func vsi(a, b int, f string, v ...interface{}) bool {
	notsame = a != b
	if notsame {
		s := fmt.Sprintf(f, v...)
		fmt.Printf("%s\n    %s\n    %s\n", s, oni(0, a), oni(1, b))
	}
	return notsame
}

// REMOVE ALL SPACE

func SpaceMap(str string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.ToLower(str))
}

func SpaceFieldsJoin(str string) string {
	return strings.Join(strings.Fields(str), "")
}

// do an HTTP request to a server and returns the response object and the
// complete response body. There's no need to close the response body as this
// will have been done.
func do(method, req_body, host, ua, uri string, extraHeaders map[string]string) (*http.Response, []byte, error) {
	var err error
	var req *http.Request
	if strings.EqualFold("POST", method) || strings.EqualFold("PUT", method) {
		req, err = http.NewRequest(method, uri, strings.NewReader(req_body))
	} else {
		req, err = http.NewRequest(method, uri, nil)
	}

	if err != nil {
		return nil, nil, err
	}
	if host != "" {
		req.Host = host
	}
	if ua != "" {
		req.Header["User-Agent"] = []string{ua}
	}
	for k, v := range extraHeaders {
		req.Header.Add(k, v)
	}

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return resp, nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	return resp, body, err
}

func parseHeader(hdr string, m map[string]string) {
	parts := strings.SplitN(hdr, ":", 2)
	if len(parts) != 2 {
		fmt.Fprintf(os.Stderr, "Bad header %s\n", hdr)
		os.Exit(2)
	}

	m[http.CanonicalHeaderKey(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
}

func main() {

	method := flag.String("method", "GET", "Sets the HTTP method")
	req_body := flag.String("body", "", "Sets body data to send server")
	host := flag.String("host", "",
		"Sets the Host header sent with both requests")
	ignore := flag.String("ignore", "",
		"Comma-separated list of headers to ignore")
	flag.BoolVar(&mono, "mono", false, "Monochrome output")
	ua := flag.String("agent", "httpdiff/0.1", "Sets User-Agent")
	help := flag.Bool("help", false, "Print usage")
	insecure := flag.Bool("insecure", false, "Allow connection to HTTPS sites without certs")
	diffapp := flag.String("diffapp", "", "The diff application to call when response bodies are different")
	header := flag.String("header", "", "Single HTTP header to add to request")
	headers := flag.String("headers", "", "File containing HTTP headers (one per line) to add to request")
	flag.Parse()

	if *help {
		fmt.Fprintf(os.Stderr, "httpdiff [options] url1 url2\n")
		flag.PrintDefaults()
		os.Exit(2)
	}

	if len(flag.Args()) != 2 {
		fmt.Printf("Must specify two URLs to test\n")
		os.Exit(2)
	}

	extraHeaders := make(map[string]string)

	if *header != "" {
		parseHeader(*header, extraHeaders)
	}

	if *headers != "" {
		file, err := os.Open(*headers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file %s: %s\n", *headers, err)
			os.Exit(2)
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			parseHeader(scanner.Text(), extraHeaders)
		}

		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file %s: %s\n", *headers, err)
			os.Exit(2)
		}

		file.Close()
	}

	exclude := make(map[string]bool)

	if *ignore != "" {
		h := strings.Split(*ignore, ",")

		for i := 0; i < len(h); i++ {
			exclude[http.CanonicalHeaderKey(h[i])] = true
		}
	}

	if *host != "" {
		fmt.Printf("Set Host to %s; ", green(*host))
	}
	vs(flag.Arg(0), flag.Arg(1)+" ", "Doing %s: ", green(*method))

	if *insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	var wg sync.WaitGroup
	var resp [2]*http.Response
	var body [2][]byte
	var err [2]error

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			resp[i], body[i], err[i] = do(*method, *req_body, *host, *ua, flag.Arg(i), extraHeaders)
			wg.Done()
		}(i)
	}

	wg.Wait()

	quit := false
	for i := 0; i < 2; i++ {
		if err[i] != nil {
			fmt.Printf("Error doing %s %s: %s\n", *method, on(i, flag.Arg(i)),
				err[i])
			quit = true
		}
	}

	if quit {
		os.Exit(2)
	}

	vsi(resp[0].StatusCode, resp[1].StatusCode, "Different status code: ")

	dmp := diffmatchpatch.New()

	fmt.Printf("Print the same object ==================   \n")
	fmt.Printf("the first %s\n", SpaceMap(string(body[0])))
	fmt.Printf("the second %s\n", SpaceMap(string(body[0])))
	fmt.Printf("END Print the same object ==================   \n")

	fmt.Printf("Print the same DiffMain on diffmatchbatch ==================   \n")
	fmt.Printf("Print the same DiffMain on diffmatchbatch ==================   \n")
	diffs := dmp.DiffMain(SpaceMap(string(body[0])), SpaceMap(string(body[1])), false)

	fmt.Printf("THE DiffToDelta!: %s\n", dmp.DiffToDelta(diffs))
	fmt.Printf("\n\n\nDiffCleanupEfficiency!: %s\n", dmp.DiffCleanupEfficiency(diffs))

	//var a []string
	//	fmt.Printf("\n\n\nTHE DiffCharsToLines!: %s\n", dmp.DiffCharsToLines(diffs, a))
	fmt.Printf("\n\n\nTHE DiffCleanupMerge!: %s\n", dmp.DiffCleanupMerge(diffs))
	fmt.Printf("\n\n\nTHE DiffLevenshtein!: %s\n", dmp.DiffLevenshtein(diffs))

	//	fmt.Printf("THE DIFFERENCES!: %s\n", dmp.MatchMain("http",'diffs))
	fmt.Printf("Print the same DiffMain on diffmatchbatch ==================   \n")
	fmt.Printf("Print the same DiffMain on diffmatchbatch ==================   \n")

	// A quick string test -- will take out but let's see
	if strings.Compare(string(body[0]), string(body[1])) == 1 {
		fmt.Printf("1111111111111111111111111111111111111111111 ==================   \n")
	} else {
		fmt.Printf("0000000000000000000000000000000000000000000000000 ==================   \n")
	}
	for h := range resp[0].Header {
		if exclude[h] {
			continue
		}
		h2 := resp[1].Header[h]
		if h2 == nil {
			continue
		}

		if !vsi(len(resp[0].Header[h]), len(resp[1].Header[h]),
			"Different number of %s headers:", green(h)) {
			for i := 0; i < len(resp[0].Header[h]); i++ {
				vs(resp[0].Header[h][i], resp[1].Header[h][i],
					"%s header different:", green(h))
			}
		}
	}

	var only [2]string

	for i := 0; i < 2; i++ {
		for h := range resp[i].Header {
			if exclude[h] {
				continue
			}
			h2 := resp[1-i].Header[h]
			if h2 == nil {
				only[i] += h
				only[i] += " "
			}
		}
	}

	if only[0] != "" || only[1] != "" {
		fmt.Printf("Unique headers\n")
		for i := 0; i < 2; i++ {
			if only[i] != "" {
				fmt.Printf("    %s\n", on(i, only[i]))
			}
		}
	}

	dump := false
	if vsi(len(body[0]), len(body[1]), "Body lengths differ:") {
		dump = true
	} else {
		if !bytes.Equal(body[0], body[1]) {
			fmt.Printf("Bodies are different\n")

		}

		dump = true
	}

	if dump {
		var temp [2]*os.File
		for i := 0; i < 2; i++ {
			var e error
			temp[i], e = ioutil.TempFile("", "httpdiff")
			if e != nil {
				fmt.Printf("Error making temporary file: %s\n", e)
				return
			}
			defer temp[i].Close()
			_, e = temp[i].Write(body[i])
			if e != nil {
				fmt.Printf("Error writing temporary file: %s\n", e)
				return
			}
			fmt.Printf("    Wrote body of %s to %s\n", on(i, flag.Arg(i)),
				on(i, temp[i].Name()))
		}

		if *diffapp != "" {
			cmd := exec.Command(*diffapp, temp[0].Name(), temp[1].Name())
			out, err := cmd.Output()
			if err != nil {
				fmt.Printf("Diff program ended with: %s\n", err)
			}
			fmt.Printf("%s\n", out)
		}
	}

	if notsame {
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}
