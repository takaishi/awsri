package awsri

import (
	"context"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
)

var Version = "dev"
var Revision = "HEAD"

type GlobalOptions struct {
}

type CLI struct {
	RDS         RDSOption         `cmd:"rds" help:"RDS"`
	Elasticache ElasticacheOption `cmd:"elasticache" help:"ElastiCache"`
	Total       TotalOption       `cmd:"total" help:"Calculate total cost of multiple RIs"`
	Version     struct{}          `cmd:"version" help:"show version"`
}

type TotalOption struct {
	RDSInstances         []string `name:"rds" help:"RDS instances in format: instance-type:count:product-description:multi-az"`
	ElasticacheInstances []string `name:"elasticache" help:"ElastiCache instances in format: node-type:count:product-description"`
	Duration             int      `name:"duration" default:"1" help:"Duration in years (1 or 3)"`
	OfferingType         string   `name:"offering-type" default:"Partial Upfront" help:"Offering type (No Upfront, Partial Upfront, All Upfront)"`
}

func RunCLI(ctx context.Context, args []string) error {
	var cli CLI
	parser, err := kong.New(&cli)
	if err != nil {
		return fmt.Errorf("error creating CLI parser: %w", err)
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		fmt.Printf("error parsing CLI: %v\n", err)
		return fmt.Errorf("error parsing CLI: %w", err)
	}
	cmd := strings.Fields(kctx.Command())[0]
	if cmd == "version" {
		fmt.Println(Version)
		return nil
	}
	return Dispatch(ctx, cmd, &cli)
}

func Dispatch(ctx context.Context, command string, cli *CLI) error {
	switch command {
	case "rds":
		cmd := NewRDSCommand(cli.RDS)
		return cmd.Run(ctx)
	case "elasticache":
		cmd := NewElastiCacheCommand(cli.Elasticache)
		return cmd.Run(ctx)
	case "total":
		cmd := NewTotalCommand(cli.Total)
		return cmd.Run(ctx)
	case "version":
		fmt.Printf("%s-%s\n", Version, Revision)
		return nil
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}
