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
	"github.com/aws/aws-sdk-go-v2/service/savingsplans"
	savingsplansTypes "github.com/aws/aws-sdk-go-v2/service/savingsplans/types"
)

type EC2Option struct {
	Region        string `name:"region" default:"ap-northeast-1" help:"AWS region"`
	InstanceType  string `name:"instance-type" required:"" help:"EC2 instance type (e.g., m5.large)"`
	Count         int    `name:"count" required:"" help:"Number of instances"`
	Duration      int    `name:"duration" default:"1" help:"Duration in years (1 or 3)"`
	PaymentOption string `name:"payment-option" default:"no-upfront" help:"Payment option (no-upfront, partial-upfront, all-upfront)"`
	NoHeader      bool   `name:"no-header" help:"Do not output CSV header"`
}

type EC2Command struct {
	opts EC2Option
}

type EC2Pricing struct {
	OnDemandPrice float64 // per hour
	SPPrice       float64 // per hour (Savings Plan)
}

func NewEC2Command(opts EC2Option) *EC2Command {
	return &EC2Command{opts: opts}
}

func (c *EC2Command) Run(ctx context.Context) error {
	// Validate duration (must be 1 or 3 years)
	if c.opts.Duration != 1 && c.opts.Duration != 3 {
		return fmt.Errorf("duration must be 1 or 3 years, got: %d", c.opts.Duration)
	}

	// Pricing API and Savings Plans API are only available in us-east-1
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %v", err)
	}

	// Get on-demand pricing
	onDemandPrice, err := c.getEC2OnDemandPrice(cfg)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	// Get Savings Plan pricing
	spPrice, err := c.getComputeSavingsPlanPrice(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get Savings Plan price: %v", err)
	}

	// Calculate hourly cost
	// Hourly commitment = number of instances × hourly SP price
	hourlyCommitment := float64(c.opts.Count) * spPrice

	// SP/RI purchase amount (USD) = Hourly commitment × 720 hours × 12 months × duration (years)
	hoursPerMonth := 720.0
	spPurchaseAmount := hourlyCommitment * hoursPerMonth * 12.0 * float64(c.opts.Duration)

	// Current cost (on-demand)
	currentCostPerMonth := float64(c.opts.Count) * onDemandPrice * hoursPerMonth

	// Cost after purchase (Savings Plan)
	spCostPerMonth := float64(c.opts.Count) * spPrice * hoursPerMonth

	// Calculate savings amount and savings rate
	savingsAmount := currentCostPerMonth - spCostPerMonth
	savingsRate := (savingsAmount / currentCostPerMonth) * 100.0

	// Output CSV
	renderCSV(hourlyCommitment, spPurchaseAmount, currentCostPerMonth, spCostPerMonth, savingsAmount, savingsRate, c.opts.NoHeader)

	return nil
}

// getEC2OnDemandPrice retrieves EC2 on-demand pricing using the Pricing API
func (c *EC2Command) getEC2OnDemandPrice(cfg aws.Config) (float64, error) {
	svc := pricing.NewFromConfig(cfg)
	location := mapRegionToLocation(c.opts.Region)

	filters := []types.Filter{
		{
			Field: aws.String("location"),
			Value: aws.String(location),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("instanceType"),
			Value: aws.String(c.opts.InstanceType),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("operatingSystem"),
			Value: aws.String("Linux"),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("tenancy"),
			Value: aws.String("Shared"),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String("preInstalledSw"),
			Value: aws.String("NA"),
			Type:  types.FilterTypeTermMatch,
		},
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters:     filters,
		MaxResults:  aws.Int32(100),
	}

	result, err := svc.GetProducts(context.TODO(), input)
	if err != nil {
		return 0, fmt.Errorf("failed to get products: %v", err)
	}

	if len(result.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing information found for instance type %s in location %s", c.opts.InstanceType, location)
	}

	// Extract pricing from the first result
	return extractOnDemandPriceFromResult(result.PriceList[0])
}

// getComputeSavingsPlanPrice retrieves EC2 Savings Plan pricing using the Savings Plans API
func (c *EC2Command) getComputeSavingsPlanPrice(ctx context.Context, cfg aws.Config) (float64, error) {
	svc := savingsplans.NewFromConfig(cfg)

	// Get payment option from arguments
	paymentOptionStr := c.opts.PaymentOption
	if paymentOptionStr == "" {
		paymentOptionStr = "no-upfront"
	}

	// Convert lowercase hyphenated value to the format expected by AWS API
	awsPaymentOption, err := convertPaymentOptionToAWSFormat(paymentOptionStr)
	if err != nil {
		return 0, err
	}

	paymentOption := savingsplansTypes.SavingsPlanPaymentOption(awsPaymentOption)

	// Get Savings Plans Offering Rates
	durationSeconds := int64(c.opts.Duration * 365 * 24 * 60 * 60) // Convert years to seconds

	input := &savingsplans.DescribeSavingsPlansOfferingRatesInput{
		SavingsPlanTypes: []savingsplansTypes.SavingsPlanType{
			savingsplansTypes.SavingsPlanTypeCompute,
		},
		Products: []savingsplansTypes.SavingsPlanProductType{
			savingsplansTypes.SavingsPlanProductTypeEc2,
		},
		ServiceCodes: []savingsplansTypes.SavingsPlanRateServiceCode{
			savingsplansTypes.SavingsPlanRateServiceCode("AmazonEC2"),
		},
		SavingsPlanPaymentOptions: []savingsplansTypes.SavingsPlanPaymentOption{
			paymentOption,
		},
		Filters: []savingsplansTypes.SavingsPlanOfferingRateFilterElement{
			{
				Name: savingsplansTypes.SavingsPlanRateFilterAttributeRegion,
				Values: []string{
					c.opts.Region,
				},
			},
			{
				Name: savingsplansTypes.SavingsPlanRateFilterAttributeInstanceType,
				Values: []string{
					c.opts.InstanceType,
				},
			},
		},
		MaxResults: 100,
	}

	result, err := svc.DescribeSavingsPlansOfferingRates(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("failed to describe savings plans offering rates: %v", err)
	}

	if len(result.SearchResults) == 0 {
		// If not found with the specified payment option, try other options
		if paymentOptionStr == "no-upfront" {
			input.SavingsPlanPaymentOptions = []savingsplansTypes.SavingsPlanPaymentOption{
				savingsplansTypes.SavingsPlanPaymentOptionAllUpfront,
			}
			result, err = svc.DescribeSavingsPlansOfferingRates(context.TODO(), input)
			if err != nil {
				return 0, fmt.Errorf("failed to describe savings plans offering rates (all-upfront): %v", err)
			}
		}
		if len(result.SearchResults) == 0 {
			return 0, fmt.Errorf("no savings plans offering rates found for payment option: %s", paymentOptionStr)
		}
	}

	// Find offers that match duration and instance type
	var matchedRate float64
	found := false

	for _, offering := range result.SearchResults {
		// Check if duration matches
		if offering.SavingsPlanOffering != nil && offering.SavingsPlanOffering.DurationSeconds != durationSeconds {
			continue
		}

		// Check if region matches
		regionCode := getRegionCodeFromLocation(offering.Properties)
		if regionCode != "" && regionCode != c.opts.Region {
			continue
		}

		// Check if instance type matches
		instanceType := c.getInstanceTypeFromProperties(offering.Properties)
		if instanceType != "" && instanceType != c.opts.InstanceType {
			continue
		}

		// Check for Linux (exclude Windows)
		if offering.UsageType != nil {
			usageType := strings.ToLower(*offering.UsageType)
			if strings.Contains(usageType, "windows") {
				continue
			}
		}

		// Also check from Properties
		isWindows := false
		for _, prop := range offering.Properties {
			if prop.Name != nil && prop.Value != nil {
				if *prop.Name == "usagetype" {
					usageType := strings.ToLower(*prop.Value)
					if strings.Contains(usageType, "windows") {
						isWindows = true
						break
					}
				}
			}
		}
		if isWindows {
			continue
		}

		// Get Rate
		if offering.Rate != nil {
			rate, err := strconv.ParseFloat(*offering.Rate, 64)
			if err == nil {
				matchedRate = rate
				found = true
				break
			}
		}
	}

	if !found {
		return 0, fmt.Errorf("Savings Plan price not found for instance type %s with duration %d years", c.opts.InstanceType, c.opts.Duration)
	}

	return matchedRate, nil
}

// getInstanceTypeFromProperties retrieves instance type from Properties
func (c *EC2Command) getInstanceTypeFromProperties(properties []savingsplansTypes.SavingsPlanOfferingRateProperty) string {
	for _, prop := range properties {
		if prop.Name != nil && *prop.Name == "instanceType" && prop.Value != nil {
			return *prop.Value
		}
	}
	return ""
}
