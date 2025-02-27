package awsri

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
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
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("ap-northeast-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config, %v", err)
	}

	tableRenderer := NewTableRenderer()
	svc := elasticache.NewFromConfig(cfg)

	// オンデマンド料金をAPI経由で取得
	onDemandPrice, err := c.getElastiCacheOnDemandPrice(cfg, c.opts.CacheNodeType, c.opts.ProductDescription)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	for _, duration := range Durations {
		durationMonths := DurationToMonths(duration)

		for _, offeringType := range OfferingTypes {
			if offeringType == "On-Demand" {
				tableRenderer.AppendOnDemandRow(duration, onDemandPrice)
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
				fixedPrice := *offering.FixedPrice

				// Calculate effective monthly cost
				effectiveMonthly := CalculateEffectiveMonthly(fixedPrice, monthlyRecurring, durationMonths)

				// Calculate savings
				monthlySavings, savingsPercent := CalculateSavings(onDemandPrice, effectiveMonthly)

				tableRenderer.AppendReservedRow(
					duration,
					offeringType,
					fixedPrice,
					monthlyRecurring,
					effectiveMonthly,
					monthlySavings,
					savingsPercent,
				)
			} else {
				tableRenderer.AppendNotAvailableRow(duration, offeringType)
			}
		}

		// 期間ごとに区切り線を追加
		if duration != Durations[len(Durations)-1] {
			tableRenderer.AppendSeparator()
		}
	}

	tableRenderer.Render()
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
			Value: aws.String("ap-northeast-1"),
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

	return extractPriceFromResult(result)
}
