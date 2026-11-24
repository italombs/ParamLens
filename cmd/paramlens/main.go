// Command paramlens profiles the parameters of SQL Server Extended Events rpc_completed
// captures. It serves a local web interface on 127.0.0.1 and shows it in its own window
// (WebView2 on Windows) or, when that is not available, in the browser.
//
//	paramlens [-port 8765] [-browser] [-no-browser] [-bind 127.0.0.1]
//	paramlens -write-html ParamLens.html   (writes the standalone offline version and exits)
//	paramlens -write-demo sample-data.tsv  (writes the sample data as an exported file and exits)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/italombs/ParamLens/internal/desktop"
	"github.com/italombs/ParamLens/internal/server"
	"github.com/italombs/ParamLens/internal/source"
	"github.com/italombs/ParamLens/web"
)

// version is set by the release build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	port := flag.Int("port", 0, "local port (0 = automatic)")
	browser := flag.Bool("browser", false, "open the interface in the default browser instead of the app window")
	noBrowser := flag.Bool("no-browser", false, "do not open any window: only serve the interface (link printed on the console)")
	bind := flag.String("bind", "127.0.0.1", "listen address (use 0.0.0.0 only inside a container, publishing the port on 127.0.0.1)")
	writeHTML := flag.String("write-html", "", "write the standalone (offline) HTML version to this file and exit")
	writeDemo := flag.String("write-demo", "", "write the synthetic sample data as an exported TSV file to this file and exit")
	demoRows := flag.Int("demo-rows", 6000, "number of events written by -write-demo")
	flag.Parse()

	window := !*browser && !*noBrowser && desktop.WindowAvailable()
	if !window || *writeHTML != "" || *writeDemo != "" {
		desktop.Console()
	}

	if *writeHTML != "" {
		if err := os.WriteFile(*writeHTML, web.Page(), 0o644); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		fmt.Println("Written:", *writeHTML)
		return
	}

	if *writeDemo != "" {
		f, err := os.Create(*writeDemo)
		if err == nil {
			err = source.WriteDemoTSV(f, *demoRows)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		fmt.Println("Written:", *writeDemo)
		return
	}

	srv, err := server.Listen(server.Options{Version: version, Port: *port, Bind: *bind})
	if err != nil {
		desktop.Fatal(err.Error())
		os.Exit(1)
	}

	if window {
		go func() {
			if err := srv.Serve(); err != nil {
				desktop.Fatal(err.Error())
				os.Exit(1)
			}
		}()
		if err := desktop.RunWindow("ParamLens", srv.URL+"&app=1"); err == nil {
			server.Cleanup()
			return
		}
		// the runtime is installed but the window could not start: fall back to the browser
		desktop.Console()
		srv.Banner()
		server.OpenBrowser(srv.URL)
		select {}
	}

	srv.Banner()
	if !*noBrowser {
		server.OpenBrowser(srv.URL)
	}
	if err := srv.Serve(); err != nil {
		desktop.Fatal(err.Error())
		os.Exit(1)
	}
}
