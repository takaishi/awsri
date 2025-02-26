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
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/olekukonko/tablewriter"
)

type ElasticacheOption struct {
	CacheNodeType      string `required:"" help:"Cache node type"`
	ProductDescription string `required:"" help:"Product description"`
}

type ElasticacheCommand struct {
	opts ElasticacheOption
}

func NewElastiCacheCommand(opts ElasticacheOption) *ElasticacheCommand {
	return &ElasticacheCommand{opts: opts}
}

func (c *ElasticacheCommand) Run(ctx context.Context) error {
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

	OfferingTypes := []string{"On-Demand", "No Upfront", "Partial Upfront", "All Upfront"}
	Durations := []int{1, 3}
	svc := elasticache.NewFromConfig(cfg)

	// オンデマンド料金をAPI経由で取得
	onDemandPrice, err := c.getElastiCacheOnDemandPrice(cfg, c.opts.CacheNodeType, c.opts.ProductDescription)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	// 各期間ごとのオンデマンドの総コストを保存
	totalCosts := make(map[string]float64)

	for _, duration := range Durations {
		for _, offeringType := range OfferingTypes {
			if offeringType == "On-Demand" {
				yearlyOnDemand := onDemandPrice * float64(12*duration)
				table.Append([]string{
					strconv.Itoa(duration),
					offeringType,
					"0",
					fmt.Sprintf("%.0f", onDemandPrice),
					fmt.Sprintf("%.0f", yearlyOnDemand),
					"",
				})
				totalCosts[strconv.Itoa(duration)] = yearlyOnDemand
				continue
			}

			params := &elasticache.DescribeReservedCacheNodesOfferingsInput{
				Duration:           aws.String(strconv.Itoa(duration)),
				OfferingType:       aws.String(offeringType),
				CacheNodeType:      aws.String(c.opts.CacheNodeType),
				ProductDescription: aws.String(c.opts.ProductDescription),
			}
			o, err := svc.DescribeReservedCacheNodesOfferings(context.TODO(), params)
			if err != nil {
				return err
			}
			if len(o.ReservedCacheNodesOfferings) > 0 {
				offering := o.ReservedCacheNodesOfferings[0]
				monthlyRecurring := *offering.RecurringCharges[0].RecurringChargeAmount * 24 * 30
				totalCost := *offering.FixedPrice + (monthlyRecurring * float64(12*duration))

				savings := ""
				if totalCosts[strconv.Itoa(duration)] > 0 {
					savingsAmount := totalCosts[strconv.Itoa(duration)] - totalCost
					savingsPercent := (savingsAmount / totalCosts[strconv.Itoa(duration)]) * 100
					savings = fmt.Sprintf("%.0f (%.1f%%)", savingsAmount, savingsPercent)
				}

				table.Append([]string{
					strconv.Itoa(duration),
					offeringType,
					fmt.Sprintf("%.0f", *offering.FixedPrice),
					fmt.Sprintf("%.0f", monthlyRecurring),
					fmt.Sprintf("%.0f", totalCost),
					savings,
				})
			} else {
				table.Append([]string{strconv.Itoa(duration), offeringType, "N/A", "N/A", "N/A", "N/A"})
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

func (c *ElasticacheCommand) getElastiCacheOnDemandPrice(cfg aws.Config, cacheNodeType string, productDescription string) (float64, error) {
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
