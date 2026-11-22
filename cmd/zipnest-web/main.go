// zipnest-web runs the ZipNest interface in a web browser (Linux, macOS, or
// for development). It prints the address to open.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"zipnest/cli"
	"zipnest/ui"
)

type host struct{ ui.BaseHost }

func (host) Open(p string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", p).Start()
	case "linux":
		return exec.Command("xdg-open", p).Start()
	}
	return host{}.BaseHost.Open(p)
}

func main() {
	noBrowser := flag.Bool("no-browser", false, "only print the address")
	action := flag.String("action", "", "start action (development)")
	flag.Parse()
	start := ""
	var args []string
	if flag.NArg() > 0 {
		start = flag.Arg(0)
		args = flag.Args()
	}
	s, err := ui.Start(ui.Options{Host: host{}, Version: cli.Version, Start: start, Action: *action, Args: args, SFXStub: cli.SFXStub})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(s.URL)
	if !*noBrowser && !strings.Contains(os.Getenv("ZIPNEST_NO_BROWSER"), "1") {
		host{}.Open(s.URL)
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	s.CleanTemp()
}
