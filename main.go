package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/gh-tui-tools/gh-rate-limit-status/internal/ratelimit"
)

type options struct {
	help  bool
	all   bool
	json  bool
	watch bool
	warn  *float64
}

// parseArgs parses CLI flags. Unknown arguments are ignored, matching the
// original tool. A bad or missing --warn value returns an error.
func parseArgs(argv []string) (options, error) {
	var o options
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "-h", "--help":
			o.help = true
		case "-a", "--all":
			o.all = true
		case "--json":
			o.json = true
		case "-w", "--watch":
			o.watch = true
		case "--warn":
			if i+1 >= len(argv) {
				return o, fmt.Errorf("--warn requires a threshold percentage")
			}
			v, err := strconv.ParseFloat(argv[i+1], 64)
			if err != nil {
				return o, fmt.Errorf("--warn requires a number")
			}
			o.warn = &v
			i++
		}
	}
	return o, nil
}

const helpText = `Usage: gh rate-limit-status [options]

Show your GitHub API rate-limit status in a user-friendly table.

Options:
  -a, --all       Show all rate limits (including low-volume ones)
  -h, --help      Show this help message
  --json          Output as JSON (for scripting)
  -w, --watch     Continuously update the display (Ctrl+C to stop)
  --warn <pct>    Exit with code 1 if any limit is below <pct>%
`

// fetchRateLimit calls GET /rate_limit and returns the raw response body.
func fetchRateLimit(client *api.RESTClient) ([]byte, error) {
	resp, err := client.Request("GET", "rate_limit", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// run executes one render (or a watch loop) and returns the process exit code.
func run(o options, out io.Writer) int {
	client, err := api.DefaultRESTClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching rate limits: %v\n", err)
		return 1
	}
	colorEnabled := term.FromEnv().IsColorEnabled()

	render := func() int {
		data, err := fetchRateLimit(client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching rate limits: %v\n", err)
			return 1
		}
		resources, err := ratelimit.ParseResources(data)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error parsing rate limit response")
			return 1
		}
		limits := ratelimit.Process(resources, o.all)
		now := time.Now()
		if o.json {
			if err := ratelimit.RenderJSON(out, limits, now); err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching rate limits: %v\n", err)
				return 1
			}
		} else {
			ratelimit.RenderTable(out, limits, now, colorEnabled)
		}
		if !o.watch && o.warn != nil && ratelimit.BelowThreshold(limits, *o.warn) {
			return 1
		}
		return 0
	}

	if !o.watch {
		return render()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	for {
		fmt.Fprint(out, "\033[2J\033[H")
		if code := render(); code != 0 {
			return code
		}
		fmt.Fprintln(out, "Press Ctrl+C to stop")
		select {
		case <-sig:
			fmt.Fprint(out, "\n\n")
			return 0
		case <-time.After(5 * time.Second):
		}
	}
}

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		os.Exit(1)
	}
	if o.help {
		fmt.Print(helpText)
		os.Exit(0)
	}
	os.Exit(run(o, os.Stdout))
}
