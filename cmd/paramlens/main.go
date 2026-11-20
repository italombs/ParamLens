// Command paramlens profiles the parameters of SQL Server Extended Events rpc_completed
// captures. It serves a local web interface on 127.0.0.1 and opens the browser.
//
//	paramlens [-port 8765] [-no-browser] [-bind 127.0.0.1]
//	paramlens -write-html ParamLens.html   (writes the standalone offline version and exits)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/italombs/ParamLens/internal/server"
	"github.com/italombs/ParamLens/web"
)

// version is set by the release build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	port := flag.Int("port", 0, "local port (0 = automatic)")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser")
	bind := flag.String("bind", "127.0.0.1", "listen address (use 0.0.0.0 only inside a container, publishing the port on 127.0.0.1)")
	writeHTML := flag.String("write-html", "", "write the standalone (offline) HTML version to this file and exit")
	flag.Parse()

	if *writeHTML != "" {
		if err := os.WriteFile(*writeHTML, web.Page(), 0o644); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		fmt.Println("Written:", *writeHTML)
		return
	}
	server.Run(server.Options{Version: version, Port: *port, Bind: *bind, NoBrowser: *noBrowser})
}
