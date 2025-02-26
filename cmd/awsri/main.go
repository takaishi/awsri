package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/takaishi/awsri"
)

var Version = "dev"
var Revision = "HEAD"

func init() {
	awsri.Version = Version
	awsri.Revision = Revision
}

func main() {
	ctx := context.TODO()
	ctx, stop := signal.NotifyContext(ctx, []os.Signal{os.Interrupt}...)
	defer stop()
	if err := awsri.RunCLI(ctx, os.Args[1:]); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}
