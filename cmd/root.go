/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
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
		HEADINGS := []string{
			"Duration",
			"Offering Type",
			"Upfront (USD)",
			"Monthly (USD)",
			"Effective Monthly (USD)",
			"Savings/Month",
		}

		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("ap-northeast-1"))
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

func getRdsOnDemandPrice(cfg aws.Config, dbInstanceClass string, productDescription string, multiAz bool) (float64, error) {
	// Pricing APIはus-east-1でのみ利用可能
	pricingCfg := cfg.Copy()
	pricingCfg.Region = "us-east-1"
	svc := pricing.NewFromConfig(pricingCfg)

	// RDSのオンデマンド料金を取得
	filters := []types.Filter{
		{
			Field: aws.String("instanceType"),
			Value: aws.String(dbInstanceClass),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("databaseEngine"),
			Value: aws.String(strings.ToLower(productDescription)),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("deploymentOption"),
			Value: aws.String(getDeploymentOption(multiAz)),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("regionCode"),
			Value: aws.String("ap-northeast-1"),
			Type:  types.FilterTypeTermMatch,
		},
	}
	fmt.Printf("filters: %v\n", filters)

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonRDS"),
		Filters:     filters,
	}

	result, err := svc.GetProducts(context.TODO(), input)
	if err != nil {
		return 0, err
	}

	if len(result.PriceList) > 0 {
		// Parse JSON response to get the price
		// This is a simplified version - you might need to handle the JSON parsing more carefully
		var priceData map[string]interface{}
		err = json.Unmarshal([]byte(result.PriceList[0]), &priceData)
		if err != nil {
			return 0, err
		}

		// Navigate through the price data structure
		terms := priceData["terms"].(map[string]interface{})
		onDemand := terms["OnDemand"].(map[string]interface{})
		for _, v := range onDemand {
			priceDimensions := v.(map[string]interface{})["priceDimensions"].(map[string]interface{})
			for _, pd := range priceDimensions {
				pricePerUnit := pd.(map[string]interface{})["pricePerUnit"].(map[string]interface{})
				price, _ := strconv.ParseFloat(pricePerUnit["USD"].(string), 64)
				return price * 24 * 30, nil // Convert to monthly price
			}
		}
	}

	return 0, fmt.Errorf("no pricing information found")
}

func getElastiCacheOnDemandPrice(cfg aws.Config, cacheNodeType string, productDescription string) (float64, error) {
	// Pricing APIはus-east-1でのみ利用可能
	pricingCfg := cfg.Copy()
	pricingCfg.Region = "us-east-1"
	svc := pricing.NewFromConfig(pricingCfg)

	// ElastiCacheのオンデマンド料金を取得
	filters := []types.Filter{
		{
			Field: aws.String("instanceType"),
			Value: aws.String(cacheNodeType),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("cacheEngine"),
			Value: aws.String(strings.ToLower(productDescription)),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("regionCode"),
			Value: aws.String("us-west-2"),
			Type:  types.FilterTypeTermMatch,
		},
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonElastiCache"),
		Filters:     filters,
	}

	result, err := svc.GetProducts(context.TODO(), input)
	if err != nil {
		return 0, err
	}

	if len(result.PriceList) > 0 {
		var priceData map[string]interface{}
		err = json.Unmarshal([]byte(result.PriceList[0]), &priceData)
		if err != nil {
			return 0, err
		}

		terms := priceData["terms"].(map[string]interface{})
		onDemand := terms["OnDemand"].(map[string]interface{})
		for _, v := range onDemand {
			priceDimensions := v.(map[string]interface{})["priceDimensions"].(map[string]interface{})
			for _, pd := range priceDimensions {
				pricePerUnit := pd.(map[string]interface{})["pricePerUnit"].(map[string]interface{})
				price, _ := strconv.ParseFloat(pricePerUnit["USD"].(string), 64)
				return price * 24 * 30, nil // Convert to monthly price
			}
		}
	}

	return 0, fmt.Errorf("no pricing information found")
}

func getDeploymentOption(multiAz bool) string {
	if multiAz {
		return "Multi-AZ"
	}
	return "Single-AZ"
}

func getRdsOffering(cfg aws.Config, table *tablewriter.Table) error {
	OfferingTypes := []string{"On-Demand", "No Upfront", "Partial Upfront", "All Upfront"}
	Durations := []string{"1", "3"}
	svc := rds.NewFromConfig(cfg)

	// オンデマンド料金をAPI経由で取得
	onDemandPrice, err := getRdsOnDemandPrice(cfg, dbInstanceClass, productDescription, multiAz)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	for _, duration := range Durations {
		durationMonths := 12 * stringToInt(duration)

		for _, offeringType := range OfferingTypes {
			if offeringType == "On-Demand" {
				table.Append([]string{
					fmt.Sprintf("%dy", stringToInt(duration)),
					offeringType,
					"0",
					fmt.Sprintf("%.0f", onDemandPrice),
					fmt.Sprintf("%.0f", onDemandPrice),
					"-",
				})
				continue
			}

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
				monthlyRecurring := *offering.RecurringCharges[0].RecurringChargeAmount * 24 * 30

				// 前払い費用を月額換算
				monthlyUpfront := *offering.FixedPrice / float64(durationMonths)
				effectiveMonthly := monthlyUpfront + monthlyRecurring

				// 月額での節約額を計算
				monthlySavings := onDemandPrice - effectiveMonthly
				savingsPercent := (monthlySavings / onDemandPrice) * 100

				table.Append([]string{
					fmt.Sprintf("%dy", stringToInt(duration)),
					offeringType,
					fmt.Sprintf("%.0f", *offering.FixedPrice),
					fmt.Sprintf("%.0f", monthlyRecurring),
					fmt.Sprintf("%.0f", effectiveMonthly),
					fmt.Sprintf("%.0f (%.1f%%)", monthlySavings, savingsPercent),
				})
			} else {
				table.Append([]string{
					fmt.Sprintf("%dy", stringToInt(duration)),
					offeringType,
					"N/A", "N/A", "N/A", "N/A",
				})
			}
		}

		// 期間ごとに区切り線を追加
		if duration != Durations[len(Durations)-1] {
			table.Append([]string{"", "", "", "", "", ""})
		}
	}
	return nil
}

func getElastiCacheOffering(cfg aws.Config, table *tablewriter.Table) error {
	OfferingTypes := []string{"On-Demand", "No Upfront", "Partial Upfront", "All Upfront"}
	Durations := []string{"1", "3"}
	svc := elasticache.NewFromConfig(cfg)

	// オンデマンド料金をAPI経由で取得
	onDemandPrice, err := getElastiCacheOnDemandPrice(cfg, cacheNodeType, productDescription)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	// 各期間ごとのオンデマンドの総コストを保存
	totalCosts := make(map[string]float64)

	for _, duration := range Durations {
		for _, offeringType := range OfferingTypes {
			if offeringType == "On-Demand" {
				yearlyOnDemand := onDemandPrice * float64(12*stringToInt(duration))
				table.Append([]string{
					duration,
					offeringType,
					"0",
					fmt.Sprintf("%.0f", onDemandPrice),
					fmt.Sprintf("%.0f", yearlyOnDemand),
					"",
				})
				totalCosts[duration] = yearlyOnDemand
				continue
			}

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
				monthlyRecurring := *offering.RecurringCharges[0].RecurringChargeAmount * 24 * 30
				totalCost := *offering.FixedPrice + (monthlyRecurring * float64(12*stringToInt(duration)))

				savings := ""
				if totalCosts[duration] > 0 {
					savingsAmount := totalCosts[duration] - totalCost
					savingsPercent := (savingsAmount / totalCosts[duration]) * 100
					savings = fmt.Sprintf("%.1f%% ($%.0f)", savingsPercent, savingsAmount)
				}

				table.Append([]string{
					duration,
					offeringType,
					fmt.Sprintf("%.0f", *offering.FixedPrice),
					fmt.Sprintf("%.0f", monthlyRecurring),
					fmt.Sprintf("%.0f", totalCost),
					savings,
				})
			} else {
				table.Append([]string{duration, offeringType, "N/A", "N/A", "N/A", "N/A"})
			}
		}
	}
	return nil
}

// 文字列を整数に変換するヘルパー関数
func stringToInt(s string) int {
	switch s {
	case "1":
		return 1
	case "3":
		return 3
	default:
		return 0
	}
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
	// rootCmd.MarkFlagRequired("db-instance-class")
	rootCmd.MarkFlagRequired("product-description")
}
