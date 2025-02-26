package awsri

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/olekukonko/tablewriter"
)

type RDSOption struct {
	DbInstanceClass    string `required:"" help:"Instance class"`
	ProductDescription string `required:"" help:"Product description"`
	MultiAz            bool   `default:"false" help:"Multi-AZ"`
}

type RDSCommand struct {
	opts RDSOption
}

func NewRDSCommand(opts RDSOption) *RDSCommand {
	return &RDSCommand{opts: opts}
}

func (c *RDSCommand) Run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("ap-northeast-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config, %v", err)
	}
	HEADINGS := []string{
		"Duration",
		"Offering Type",
		"Upfront (USD)",
		"Monthly (USD)",
		"Effective Monthly (USD)",
		"Savings/Month",
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(HEADINGS)
	table.SetAutoFormatHeaders(false)
	table.SetAutoWrapText(false)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")

	OfferingTypes := []string{"On-Demand", "No Upfront", "Partial Upfront", "All Upfront"}
	Durations := []int{1, 3}
	svc := rds.NewFromConfig(cfg)

	// オンデマンド料金をAPI経由で取得
	onDemandPrice, err := c.getRdsOnDemandPrice(cfg, c.opts.DbInstanceClass, c.opts.ProductDescription, c.opts.MultiAz)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	for _, duration := range Durations {
		durationMonths := 12 * duration

		for _, offeringType := range OfferingTypes {
			if offeringType == "On-Demand" {
				table.Append([]string{
					fmt.Sprintf("%dy", duration),
					offeringType,
					"0",
					fmt.Sprintf("%.0f", onDemandPrice),
					fmt.Sprintf("%.0f", onDemandPrice),
					"-",
				})
				continue
			}

			params := &rds.DescribeReservedDBInstancesOfferingsInput{
				Duration:           aws.String(strconv.Itoa(duration)),
				OfferingType:       aws.String(offeringType),
				DBInstanceClass:    aws.String(c.opts.DbInstanceClass),
				ProductDescription: aws.String(c.opts.ProductDescription),
				MultiAZ:            aws.Bool(c.opts.MultiAz),
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
					fmt.Sprintf("%dy", duration),
					offeringType,
					fmt.Sprintf("%.0f", *offering.FixedPrice),
					fmt.Sprintf("%.0f", monthlyRecurring),
					fmt.Sprintf("%.0f", effectiveMonthly),
					fmt.Sprintf("%.0f (%.1f%%)", monthlySavings, savingsPercent),
				})
			} else {
				table.Append([]string{
					fmt.Sprintf("%dy", duration),
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

	table.Render()
	return nil
}

func (c *RDSCommand) getRdsOnDemandPrice(cfg aws.Config, dbInstanceClass string, productDescription string, multiAz bool) (float64, error) {
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
			Value: aws.String(c.getDeploymentOption(multiAz)),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("regionCode"),
			Value: aws.String("ap-northeast-1"),
			Type:  types.FilterTypeTermMatch,
		},
	}

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

func (c *RDSCommand) getDeploymentOption(multiAz bool) string {
	if multiAz {
		return "Multi-AZ"
	}
	return "Single-AZ"
}
