/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"os"
)

const RDS = "rds"
const ELASTICACHE = "elasticache"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "awsri",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	RunE: func(cmd *cobra.Command, args []string) error {
		HEADINGS := []string{"Duration (Year)", "Offering Type", "One Time Payment (USD)", "Usage Charges (USD, Monthly)"}

		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-west-2"))
		if err != nil {
			return fmt.Errorf("unable to load SDK config, %v", err)
		}

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader(HEADINGS)
		table.SetAutoFormatHeaders(false)
		table.SetAutoWrapText(false)
		table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
		table.SetCenterSeparator("|")

		switch service {
		case RDS:
			err = getRdsOffering(cfg, table)
			if err != nil {
				return err
			}
			break
		case ELASTICACHE:
			err = getElastiCacheOffering(cfg, table)
			if err != nil {
				return err
			}
			break
		default:

		}

		table.Render()

		return nil
	},
}

func getRdsOffering(cfg aws.Config, table *tablewriter.Table) error {
	OfferingTypes := []string{"No Upfront", "Partial Upfront", "All Upfront"}
	Durations := []string{"1", "3"}
	svc := rds.NewFromConfig(cfg)

	for _, duration := range Durations {
		for _, offeringType := range OfferingTypes {
			params := &rds.DescribeReservedDBInstancesOfferingsInput{
				Duration:           aws.String(duration),
				OfferingType:       aws.String(offeringType),
				DBInstanceClass:    aws.String(dbInstanceClass),
				ProductDescription: aws.String(productDescription),
				MultiAZ:            aws.Bool(multiAz),
			}
			o, err := svc.DescribeReservedDBInstancesOfferings(context.TODO(), params)
			if err != nil {
				return err
			}
			if len(o.ReservedDBInstancesOfferings) > 0 {
				offering := o.ReservedDBInstancesOfferings[0]
				table.Append([]string{
					duration,
					offeringType,
					fmt.Sprintf("%.0f", *offering.FixedPrice),
					fmt.Sprintf("%.0f", *offering.RecurringCharges[0].RecurringChargeAmount*24*30),
				})
			} else {
				table.Append([]string{duration, offeringType, "N/A", "N/A"})
			}
		}
	}
	return nil
}

func getElastiCacheOffering(cfg aws.Config, table *tablewriter.Table) error {
	OfferingTypes := []string{"No Upfront", "Partial Upfront", "All Upfront"}
	Durations := []string{"1", "3"}
	svc := elasticache.NewFromConfig(cfg)

	for _, duration := range Durations {
		for _, offeringType := range OfferingTypes {
			params := &elasticache.DescribeReservedCacheNodesOfferingsInput{
				Duration:           aws.String(duration),
				OfferingType:       aws.String(offeringType),
				CacheNodeType:      aws.String(cacheNodeType),
				ProductDescription: aws.String(productDescription),
			}
			o, err := svc.DescribeReservedCacheNodesOfferings(context.TODO(), params)
			if err != nil {
				return err
			}
			if len(o.ReservedCacheNodesOfferings) > 0 {
				offering := o.ReservedCacheNodesOfferings[0]
				table.Append([]string{
					duration,
					offeringType,
					fmt.Sprintf("%.0f", *offering.FixedPrice),
					fmt.Sprintf("%.0f", *offering.RecurringCharges[0].RecurringChargeAmount*24*30),
				})
			} else {
				table.Append([]string{duration, offeringType, "N/A", "N/A"})
			}
		}
	}
	return nil
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var service string
var dbInstanceClass string
var productDescription string
var multiAz bool
var cacheNodeType string

func init() {
	rootCmd.Flags().StringVar(&service, "service", "", "")
	rootCmd.Flags().StringVar(&dbInstanceClass, "db-instance-class", "", "")
	rootCmd.Flags().StringVar(&productDescription, "product-description", "", "")
	rootCmd.Flags().BoolVar(&multiAz, "multi-az", true, "")
	rootCmd.Flags().StringVar(&cacheNodeType, "cache-node-type", "", "")

	rootCmd.MarkFlagRequired("service")
	rootCmd.MarkFlagRequired("db-instance-class")
	rootCmd.MarkFlagRequired("product-description")
}
