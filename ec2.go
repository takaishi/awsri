package awsri

import (
	"context"
	"encoding/json"
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
	// Pricing APIとSavings Plans APIはus-east-1でのみ利用可能
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %v", err)
	}

	// オンデマンド料金を取得
	onDemandPrice, err := c.getEC2OnDemandPrice(cfg)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	// Savings Plan料金を取得
	spPrice, err := c.getComputeSavingsPlanPrice(cfg)
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
	c.renderCSV(hourlyCommitment, spPurchaseAmount, currentCostPerMonth, spCostPerMonth, savingsAmount, savingsRate, c.opts.NoHeader)

	return nil
}

// getEC2OnDemandPrice retrieves EC2 on-demand pricing using the Pricing API
func (c *EC2Command) getEC2OnDemandPrice(cfg aws.Config) (float64, error) {
	svc := pricing.NewFromConfig(cfg)
	location := c.mapRegionToLocation(c.opts.Region)

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
	return c.extractEC2OnDemandPriceFromResult(result.PriceList[0])
}

// extractEC2OnDemandPriceFromResult extracts EC2 on-demand pricing from Pricing API response
func (c *EC2Command) extractEC2OnDemandPriceFromResult(priceListEntry string) (float64, error) {
	var priceData map[string]interface{}
	err := json.Unmarshal([]byte(priceListEntry), &priceData)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal price data: %v", err)
	}

	// OnDemand料金を取得
	terms, ok := priceData["terms"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("terms not found in pricing data")
	}

	onDemand, ok := terms["OnDemand"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("OnDemand terms not found")
	}

	for _, v := range onDemand {
		termData, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		priceDimensions, ok := termData["priceDimensions"].(map[string]interface{})
		if !ok {
			continue
		}

		for _, pd := range priceDimensions {
			dimensionData, ok := pd.(map[string]interface{})
			if !ok {
				continue
			}

			pricePerUnit, ok := dimensionData["pricePerUnit"].(map[string]interface{})
			if !ok {
				continue
			}

			// Check unit field (convert from seconds to hours if needed)
			unit, _ := dimensionData["unit"].(string)

			if usdPrice, ok := pricePerUnit["USD"].(string); ok {
				price, err := strconv.ParseFloat(usdPrice, 64)
				if err != nil {
					continue
				}

				// Convert from seconds to hours if unit is in seconds (seconds × 3600 = hours)
				if strings.Contains(strings.ToLower(unit), "second") || strings.Contains(strings.ToLower(unit), "sec") {
					price = price * 3600.0
				}

				return price, nil // Return price per hour
			}
		}
	}

	return 0, fmt.Errorf("price not found in pricing data")
}

// getComputeSavingsPlanPrice retrieves EC2 Savings Plan pricing using the Savings Plans API
func (c *EC2Command) getComputeSavingsPlanPrice(cfg aws.Config) (float64, error) {
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

	result, err := svc.DescribeSavingsPlansOfferingRates(context.TODO(), input)
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
		regionCode := c.getRegionCodeFromLocation(offering.Properties)
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
		for _, prop := range offering.Properties {
			if prop.Name != nil && prop.Value != nil {
				if *prop.Name == "usagetype" {
					usageType := strings.ToLower(*prop.Value)
					if strings.Contains(usageType, "windows") {
						continue
					}
				}
			}
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

// getRegionCodeFromLocation retrieves region code from Properties
func (c *EC2Command) getRegionCodeFromLocation(properties []savingsplansTypes.SavingsPlanOfferingRateProperty) string {
	for _, prop := range properties {
		if prop.Name != nil && *prop.Name == "regionCode" && prop.Value != nil {
			return *prop.Value
		}
		if prop.Name != nil && *prop.Name == "location" && prop.Value != nil {
			// Reverse lookup region code from location
			return c.mapLocationToRegion(*prop.Value)
		}
	}
	return ""
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

// mapLocationToRegion retrieves region code from location name
func (c *EC2Command) mapLocationToRegion(location string) string {
	locationMap := map[string]string{
		"Asia Pacific (Tokyo)":     "ap-northeast-1",
		"US East (N. Virginia)":    "us-east-1",
		"US West (Oregon)":         "us-west-2",
		"EU (Ireland)":             "eu-west-1",
		"Asia Pacific (Singapore)": "ap-southeast-1",
		"Asia Pacific (Sydney)":    "ap-southeast-2",
		"EU (Frankfurt)":           "eu-central-1",
	}
	if region, ok := locationMap[location]; ok {
		return region
	}
	return ""
}

func (c *EC2Command) mapRegionToLocation(region string) string {
	// Map region name to Pricing API location format
	locationMap := map[string]string{
		"ap-northeast-1": "Asia Pacific (Tokyo)",
		"us-east-1":      "US East (N. Virginia)",
		"us-west-2":      "US West (Oregon)",
		"eu-west-1":      "EU (Ireland)",
		"ap-southeast-1": "Asia Pacific (Singapore)",
		"ap-southeast-2": "Asia Pacific (Sydney)",
		"eu-central-1":   "EU (Frankfurt)",
	}
	if location, ok := locationMap[region]; ok {
		return location
	}
	// Default: use region name as is
	return region
}

func (c *EC2Command) renderCSV(hourlyCommitment, spPurchaseAmount, currentCost, spCost, savingsAmount, savingsRate float64, noHeader bool) {
	// Output CSV header (only if noHeader is false)
	if !noHeader {
		fmt.Println("Hourly commitment,購入するSP/RI (USD),現在のコスト(USD/月),購入後のコスト(USD/月),削減コスト,削減率")
	}

	// Output data row
	// hourly commitment doesn't need rounding, others don't need decimal places
	fmt.Printf("%g,%.0f,%.0f,%.0f,%.0f,%.0f\n",
		hourlyCommitment,
		spPurchaseAmount,
		currentCost,
		spCost,
		savingsAmount,
		savingsRate,
	)
}
