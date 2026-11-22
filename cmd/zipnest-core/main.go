// zipnest-core is the archive engine as a standalone command-line program.
// It is bundled inside the Android APK (as lib/<abi>/libzipnest.so so that
// Android installs it as an executable) and also works as a CLI on any OS.
//
// "zipnest-core serve" runs the ZipNest interface server for the Android app:
// it prints {"url": "..."} on the first line and runs until stdin is closed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"zipnest/cli"
	"zipnest/ui"
)

type androidHost struct {
	ui.BaseHost
	places []ui.Place
}

func (androidHost) Platform() string    { return "android" }
func (h androidHost) Places() []ui.Place { return h.places }

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	platform := fs.String("platform", "android", "platform name")
	data := fs.String("data", "", "folder for preferences")
	temp := fs.String("temp", "", "folder for temporary files")
	places := fs.String("places", "", "sidebar places as JSON [{name,path,icon}]")
	start := fs.String("start", "", "file or folder to show first")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	h := androidHost{}
	if *places != "" {
		json.Unmarshal([]byte(*places), &h.places)
	}
	var host ui.Host = h
	if *platform != "android" {
		host = ui.BaseHost{}
	}
	s, err := ui.Start(ui.Options{Host: host, Version: cli.Version, DataDir: *data, TempDir: *temp, Start: *start})
	if err != nil {
		fmt.Println(`{"error":` + jsonString(err.Error()) + `}`)
		return 1
	}
	b, _ := json.Marshal(map[string]string{"url": s.URL, "token": s.Token()})
	fmt.Println(string(b))
	io.Copy(io.Discard, os.Stdin) // the app closes our stdin when it goes away
	s.CleanTemp()
	return 0
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		os.Exit(serve(os.Args[2:]))
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
