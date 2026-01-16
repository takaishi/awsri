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

type FargateOption struct {
	Region          string  `name:"region" default:"ap-northeast-1" help:"AWS region"`
	MemoryMBPerHour float64 `required:"" help:"Memory MB per hour (will be converted to GB)"`
	VCPUMillicoresPerHour float64 `required:"" help:"vCPU millicores per hour (will be converted to vCPU)"`
	TaskCount       int     `required:"" help:"Number of tasks"`
	Duration        int     `name:"duration" default:"1" help:"Duration in years (1 or 3)"`
	Architecture    string  `name:"architecture" default:"x86_64" help:"Architecture (x86_64 or arm)"`
	PaymentOption   string  `name:"payment-option" default:"no-upfront" help:"Payment option (no-upfront, partial-upfront, all-upfront)"`
	NoHeader        bool    `name:"no-header" help:"Do not output CSV header"`
}

type FargateCommand struct {
	opts FargateOption
}

type FargatePricing struct {
	VCPUOnDemandPrice   float64 // per hour
	MemoryOnDemandPrice float64 // per GB per hour
	VCPUSPPrice         float64 // per hour (Savings Plan)
	MemorySPPrice       float64 // per GB per hour (Savings Plan)
}

func NewFargateCommand(opts FargateOption) *FargateCommand {
	return &FargateCommand{opts: opts}
}

func (c *FargateCommand) Run(ctx context.Context) error {
	// Pricing API and Savings Plans API are only available in us-east-1
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %v", err)
	}

	// Get on-demand pricing
	onDemandPricing, err := c.getFargateOnDemandPrice(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	// Get Savings Plan pricing
	spPricing, err := c.getComputeSavingsPlanPrice(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get Savings Plan price: %v", err)
	}

	// Validate duration (must be 1 or 3 years)
	if c.opts.Duration != 1 && c.opts.Duration != 3 {
		return fmt.Errorf("duration must be 1 or 3 years, got: %d", c.opts.Duration)
	}

	// Convert input parameter units
	// vCPU: millicores to vCPU (divide by 1000, following Kubernetes convention)
	// Memory: MB to GB (divide by 1024)
	vcpuCount := c.opts.VCPUMillicoresPerHour / 1000.0
	memoryGB := c.opts.MemoryMBPerHour / 1024.0

	// Calculate monthly cost (720 hours/month)
	// TaskCount × vCPU count × 720 hours × vCPU price + TaskCount × GB count × 720 hours × GB price
	hoursPerMonth := 720.0
	currentCostPerMonth := float64(c.opts.TaskCount)*vcpuCount*hoursPerMonth*onDemandPricing.VCPUOnDemandPrice +
		float64(c.opts.TaskCount)*memoryGB*hoursPerMonth*onDemandPricing.MemoryOnDemandPrice

	spCostPerMonth := float64(c.opts.TaskCount)*vcpuCount*hoursPerMonth*spPricing.VCPUSPPrice +
		float64(c.opts.TaskCount)*memoryGB*hoursPerMonth*spPricing.MemorySPPrice

	// Calculate hourly cost (for Hourly commitment)
	hourlySPCost := spCostPerMonth / hoursPerMonth

	// Hourly commitment = hourly cost after applying Savings Plan
	hourlyCommitment := hourlySPCost

	// SP/RI purchase amount (USD) = Hourly commitment × 720 hours × 12 months × duration (years)
	spPurchaseAmount := hourlyCommitment * hoursPerMonth * 12.0 * float64(c.opts.Duration)

	// Calculate savings amount and savings rate
	savingsAmount := currentCostPerMonth - spCostPerMonth
	savingsRate := (savingsAmount / currentCostPerMonth) * 100.0

	// Output CSV
	renderCSV(hourlyCommitment, spPurchaseAmount, currentCostPerMonth, spCostPerMonth, savingsAmount, savingsRate, c.opts.NoHeader)

	return nil
}

// getFargateOnDemandPrice retrieves Fargate on-demand pricing using the Pricing API
func (c *FargateCommand) getFargateOnDemandPrice(ctx context.Context, cfg aws.Config) (*FargatePricing, error) {
	svc := pricing.NewFromConfig(cfg)
	location := mapRegionToLocation(c.opts.Region)

	// Add architecture-based filter
	processorArchitecture := "x86_64"
	if c.opts.Architecture == "arm" {
		processorArchitecture = "ARM"
	}

	// Get vCPU pricing (using cputype=perCPU filter and architecture filter)
	vcpuPrice, err := c.getFargateOnDemandPriceByType(ctx, svc, location, "cputype", "perCPU", processorArchitecture)
	if err != nil {
		return nil, fmt.Errorf("failed to get vCPU price: %v", err)
	}

	// Get memory pricing (using memorytype=perGB filter and architecture filter)
	memoryPrice, err := c.getFargateOnDemandPriceByType(ctx, svc, location, "memorytype", "perGB", processorArchitecture)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory price: %v", err)
	}

	return &FargatePricing{
		VCPUOnDemandPrice:   vcpuPrice,
		MemoryOnDemandPrice: memoryPrice,
	}, nil
}

// getFargateOnDemandPriceByType retrieves Fargate on-demand pricing with the specified filter type
func (c *FargateCommand) getFargateOnDemandPriceByType(ctx context.Context, svc *pricing.Client, location, filterType, filterValue, processorArchitecture string) (float64, error) {
	// First, search without architecture filter
	filters := []types.Filter{
		{
			Field: aws.String("location"),
			Value: aws.String(location),
			Type:  types.FilterTypeTermMatch,
		},
		{
			Field: aws.String(filterType),
			Value: aws.String(filterValue),
			Type:  types.FilterTypeTermMatch,
		},
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonECS"),
		Filters:     filters,
		MaxResults:  aws.Int32(100),
	}

	result, err := svc.GetProducts(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("failed to get products: %v", err)
	}

	if len(result.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing information found for %s=%s in location %s", filterType, filterValue, location)
	}

	// Filter by architecture
	// In Pricing API responses, the processorArchitecture attribute may be empty,
	// so architecture information is included in usagetype (e.g., APN1-Fargate-ARM-vCPU-Hours:perCPU)
	var matchedPrice string

	for _, priceListEntry := range result.PriceList {
		var priceData map[string]interface{}
		if err := json.Unmarshal([]byte(priceListEntry), &priceData); err != nil {
			continue
		}

		product, ok := priceData["product"].(map[string]interface{})
		if !ok {
			continue
		}

		attributes, ok := product["attributes"].(map[string]interface{})
		if !ok {
			continue
		}

		// Check architecture using multiple attribute names
		arch := ""
		if val, ok := attributes["processorArchitecture"].(string); ok {
			arch = val
		} else if val, ok := attributes["ProcessorArchitecture"].(string); ok {
			arch = val
		} else if val, ok := attributes["processor"].(string); ok {
			arch = val
		}

		usagetype, _ := attributes["usagetype"].(string)

		// For ARM, also check if usagetype contains "ARM"
		if processorArchitecture == "ARM" {
			if strings.Contains(strings.ToUpper(usagetype), "ARM") || arch == "ARM" {
				matchedPrice = priceListEntry
				break
			}
		} else if arch == processorArchitecture {
			// For x86_64, look for usagetype that does not contain ARM
			if !strings.Contains(strings.ToUpper(usagetype), "ARM") {
				matchedPrice = priceListEntry
				break
			}
		}
	}

	if matchedPrice != "" {
		return extractOnDemandPriceFromResult(matchedPrice)
	}

	// If architecture doesn't match, use the first result (fallback)
	if len(result.PriceList) > 0 {
		return extractOnDemandPriceFromResult(result.PriceList[0])
	}

	return 0, fmt.Errorf("no pricing information found")
}

// convertPaymentOptionToAWSFormat converts lowercase hyphenated payment option to the format expected by AWS API
func convertPaymentOptionToAWSFormat(option string) (string, error) {
	optionMap := map[string]string{
		"no-upfront":      "No Upfront",
		"partial-upfront": "Partial Upfront",
		"all-upfront":     "All Upfront",
	}

	if awsFormat, ok := optionMap[option]; ok {
		return awsFormat, nil
	}

	return "", fmt.Errorf("invalid payment option: %s (must be one of: no-upfront, partial-upfront, all-upfront)", option)
}

// getComputeSavingsPlanPrice retrieves Fargate Savings Plan pricing using the Savings Plans API
func (c *FargateCommand) getComputeSavingsPlanPrice(ctx context.Context, cfg aws.Config) (*FargatePricing, error) {
	svc := savingsplans.NewFromConfig(cfg)

	// Get payment option from arguments
	paymentOptionStr := c.opts.PaymentOption
	// Set default value
	if paymentOptionStr == "" {
		paymentOptionStr = "no-upfront"
	}

	// Convert lowercase hyphenated value to the format expected by AWS API
	awsPaymentOption, err := convertPaymentOptionToAWSFormat(paymentOptionStr)
	if err != nil {
		return nil, err
	}

	paymentOption := savingsplansTypes.SavingsPlanPaymentOption(awsPaymentOption)

	// Get Savings Plans Offering Rates
	// Add region filter
	input := &savingsplans.DescribeSavingsPlansOfferingRatesInput{
		SavingsPlanTypes: []savingsplansTypes.SavingsPlanType{
			savingsplansTypes.SavingsPlanTypeCompute,
		},
		Products: []savingsplansTypes.SavingsPlanProductType{
			savingsplansTypes.SavingsPlanProductTypeFargate,
		},
		ServiceCodes: []savingsplansTypes.SavingsPlanRateServiceCode{
			savingsplansTypes.SavingsPlanRateServiceCode("AmazonECS"),
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
		},
		MaxResults: 100,
	}

	result, err := svc.DescribeSavingsPlansOfferingRates(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe savings plans offering rates: %v", err)
	}

	if len(result.SearchResults) == 0 {
		// If not found with the specified payment option, try other options
		// If not found with no-upfront, try all-upfront
		if paymentOptionStr == "no-upfront" {
			input.SavingsPlanPaymentOptions = []savingsplansTypes.SavingsPlanPaymentOption{
				savingsplansTypes.SavingsPlanPaymentOptionAllUpfront,
			}
			result, err = svc.DescribeSavingsPlansOfferingRates(context.TODO(), input)
			if err != nil {
				return nil, fmt.Errorf("failed to describe savings plans offering rates (all-upfront): %v", err)
			}
		}
		if len(result.SearchResults) == 0 {
			return nil, fmt.Errorf("no savings plans offering rates found for payment option: %s", paymentOptionStr)
		}
	}

	// Filter offers by duration
	durationSeconds := int64(c.opts.Duration * 365 * 24 * 60 * 60) // Convert years to seconds

	// Filtering conditions based on architecture
	isARM := c.opts.Architecture == "arm"

	var vcpuPrice, memoryPrice float64
	foundVCPU := false
	foundMemory := false

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

		// Check UsageType and Rate
		// First, check offering.UsageType
		if offering.UsageType != nil {
			usageType := strings.ToLower(*offering.UsageType)

			// Exclude Windows
			if strings.Contains(usageType, "windows") {
				continue
			}

			// Check architecture match
			hasARM := strings.Contains(usageType, "arm")
			if isARM && !hasARM {
				continue // Skip UsageType that doesn't contain ARM when ARM is specified
			}
			if !isARM && hasARM {
				continue // Skip UsageType that contains ARM when Linux x86_64 is specified
			}

			// Determine if UsageType is vCPU or Memory
			if strings.Contains(usageType, "vcpu") || strings.Contains(usageType, "cpu") {
				if offering.Rate != nil && !foundVCPU {
					rate, err := strconv.ParseFloat(*offering.Rate, 64)
					if err == nil {
						vcpuPrice = rate
						foundVCPU = true
					}
				}
			} else if strings.Contains(usageType, "gb") || strings.Contains(usageType, "memory") {
				if offering.Rate != nil && !foundMemory {
					rate, err := strconv.ParseFloat(*offering.Rate, 64)
					if err == nil {
						memoryPrice = rate
						foundMemory = true
					}
				}
			}
		}

		// Also check UsageType from Properties
		for _, prop := range offering.Properties {
			if prop.Name != nil && prop.Value != nil {
				// Check UsageType
				if *prop.Name == "usagetype" {
					usageType := strings.ToLower(*prop.Value)

					// Exclude Windows
					if strings.Contains(usageType, "windows") {
						continue
					}

					// Check architecture match
					hasARM := strings.Contains(usageType, "arm")
					if isARM && !hasARM {
						continue // Skip UsageType that doesn't contain ARM when ARM is specified
					}
					if !isARM && hasARM {
						continue // Skip UsageType that contains ARM when Linux x86_64 is specified
					}

					// Determine if UsageType is vCPU or Memory
					if strings.Contains(usageType, "vcpu") || strings.Contains(usageType, "cpu") {
						if offering.Rate != nil && !foundVCPU {
							rate, err := strconv.ParseFloat(*offering.Rate, 64)
							if err == nil {
								vcpuPrice = rate
								foundVCPU = true
							}
						}
					} else if strings.Contains(usageType, "gb") || strings.Contains(usageType, "memory") {
						if offering.Rate != nil && !foundMemory {
							rate, err := strconv.ParseFloat(*offering.Rate, 64)
							if err == nil {
								memoryPrice = rate
								foundMemory = true
							}
						}
					}
				}
			}
		}

		// Also check unit field
		unit := strings.ToLower(string(offering.Unit))
		if strings.Contains(unit, "hour") || strings.Contains(unit, "hr") {
			// This is hourly pricing
			if offering.Rate != nil {
				rate, err := strconv.ParseFloat(*offering.Rate, 64)
				if err == nil {
					// If cannot determine from UsageType, infer from unit and rate
					// Usually vCPU is higher than memory
					if !foundVCPU && rate > 0.01 {
						vcpuPrice = rate
						foundVCPU = true
					} else if !foundMemory && rate < 0.01 {
						memoryPrice = rate
						foundMemory = true
					}
				}
			}
		}
	}

	// If not found, search all results to find the first vCPU and Memory
	if !foundVCPU || !foundMemory {
		for _, offering := range result.SearchResults {
			if offering.SavingsPlanOffering != nil && offering.SavingsPlanOffering.DurationSeconds != durationSeconds {
				continue
			}

			regionCode := getRegionCodeFromLocation(offering.Properties)
			if regionCode != "" && regionCode != c.opts.Region {
				continue
			}

			if offering.Rate != nil {
				rate, err := strconv.ParseFloat(*offering.Rate, 64)
				if err != nil {
					continue
				}

				// Determine from UsageType
				if offering.UsageType != nil {
					usageType := strings.ToLower(*offering.UsageType)
					if (strings.Contains(usageType, "vcpu") || strings.Contains(usageType, "cpu")) && !foundVCPU {
						vcpuPrice = rate
						foundVCPU = true
					} else if (strings.Contains(usageType, "gb") || strings.Contains(usageType, "memory")) && !foundMemory {
						memoryPrice = rate
						foundMemory = true
					}
				}

				// Also check UsageType from Properties
				for _, prop := range offering.Properties {
					if prop.Name != nil && prop.Value != nil {
						if *prop.Name == "usagetype" {
							usageType := strings.ToLower(*prop.Value)
							if (strings.Contains(usageType, "vcpu") || strings.Contains(usageType, "cpu")) && !foundVCPU {
								vcpuPrice = rate
								foundVCPU = true
							} else if (strings.Contains(usageType, "gb") || strings.Contains(usageType, "memory")) && !foundMemory {
								memoryPrice = rate
								foundMemory = true
							}
						}
					}
				}

				// If neither found, infer from rate value
				if !foundVCPU && !foundMemory {
					if rate > 0.01 {
						vcpuPrice = rate
						foundVCPU = true
					} else {
						memoryPrice = rate
						foundMemory = true
					}
				}
			}
		}
	}

	if !foundVCPU {
		return nil, fmt.Errorf("vCPU Savings Plan price not found")
	}
	if !foundMemory {
		return nil, fmt.Errorf("memory Savings Plan price not found")
	}

	return &FargatePricing{
		VCPUSPPrice:   vcpuPrice,
		MemorySPPrice: memoryPrice,
	}, nil
}
