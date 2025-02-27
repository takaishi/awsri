package awsri

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
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

	tableRenderer := NewTableRenderer()
	svc := rds.NewFromConfig(cfg)

	// オンデマンド料金をAPI経由で取得
	databaseEngine, err := c.getDatabaseEngine(c.opts.ProductDescription)
	if err != nil {
		return fmt.Errorf("failed to get database engine: %v", err)
	}
	onDemandPrice, err := c.getRdsOnDemandPrice(cfg, c.opts.DbInstanceClass, databaseEngine, c.opts.MultiAz)
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
				offering := c.getOffering(o.ReservedDBInstancesOfferings, c.opts.ProductDescription, c.opts.MultiAz)
				if offering == nil {
					tableRenderer.AppendNotAvailableRow(duration, offeringType)
					continue
				}

				monthlyRecurring := *offering.RecurringCharges[0].RecurringChargeAmount * 24 * 30
				fixedPrice := *offering.FixedPrice

				// Calculate effective yearly cost (function name remains the same for compatibility)
				effectiveYearly := CalculateEffectiveMonthly(fixedPrice, monthlyRecurring, durationMonths)

				// Calculate yearly savings
				yearlySavings, savingsPercent := CalculateSavings(onDemandPrice, effectiveYearly)

				tableRenderer.AppendReservedRow(
					duration,
					offeringType,
					fixedPrice,
					monthlyRecurring,
					effectiveYearly,
					yearlySavings,
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

	return extractPriceFromResult(result)
}

func (c *RDSCommand) getDeploymentOption(multiAz bool) string {
	if multiAz {
		return "Multi-AZ"
	}
	return "Single-AZ"
}

func (c *RDSCommand) getOffering(offerings []rdsTypes.ReservedDBInstancesOffering, productDescription string, multiAz bool) *rdsTypes.ReservedDBInstancesOffering {
	for _, offering := range offerings {
		if *offering.ProductDescription == productDescription && *offering.MultiAZ == multiAz {
			return &offering
		}
	}
	return nil
}

func (c *RDSCommand) getDatabaseEngine(productDescription string) (string, error) {
	if strings.Contains(productDescription, "postgresql") {
		return "PostgreSQL", nil
	}
	return "", fmt.Errorf("unsupported database engine: %s", productDescription)
}
