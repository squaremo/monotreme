package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dpw/monotreme/comms"
)

func main() {
	var bindAddr string
	flag.StringVar(&bindAddr, "b", ":8080", "bind address")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Synopsis:\n  %s [options] peer...\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	nd, err := comms.NewNodeDaemon(bindAddr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, arg := range flag.Args() {
		err := nd.Connect(arg)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	var wait chan struct{}

	for {
		<-wait
	}
}
