package awsri

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	savingsplansTypes "github.com/aws/aws-sdk-go-v2/service/savingsplans/types"
)

// mapLocationToRegion maps location name to region code
func mapLocationToRegion(location string) string {
	locationMap := map[string]string{
		"Asia Pacific (Tokyo)":       "ap-northeast-1",
		"Asia Pacific (Seoul)":       "ap-northeast-2",
		"Asia Pacific (Osaka)":       "ap-northeast-3",
		"Asia Pacific (Mumbai)":      "ap-south-1",
		"Asia Pacific (Singapore)":   "ap-southeast-1",
		"Asia Pacific (Sydney)":      "ap-southeast-2",
		"Asia Pacific (Jakarta)":     "ap-southeast-3",
		"Asia Pacific (Melbourne)":   "ap-southeast-4",
		"Canada (Central)":           "ca-central-1",
		"EU (Frankfurt)":             "eu-central-1",
		"EU (Ireland)":               "eu-west-1",
		"EU (London)":                "eu-west-2",
		"EU (Paris)":                 "eu-west-3",
		"EU (Milan)":                 "eu-south-1",
		"EU (Stockholm)":             "eu-north-1",
		"EU (Spain)":                 "eu-south-2",
		"EU (Zurich)":                "eu-central-2",
		"Middle East (Bahrain)":      "me-south-1",
		"Middle East (UAE)":          "me-central-1",
		"South America (São Paulo)":  "sa-east-1",
		"US East (N. Virginia)":      "us-east-1",
		"US East (Ohio)":             "us-east-2",
		"US West (N. California)":   "us-west-1",
		"US West (Oregon)":           "us-west-2",
		"Africa (Cape Town)":         "af-south-1",
		"Asia Pacific (Hong Kong)":   "ap-east-1",
		"China (Beijing)":            "cn-north-1",
		"China (Ningxia)":            "cn-northwest-1",
		"Israel (Tel Aviv)":          "il-central-1",
	}
	if region, ok := locationMap[location]; ok {
		return region
	}
	return ""
}

// mapRegionToLocation maps region code to location name for Pricing API
func mapRegionToLocation(region string) string {
	regionMap := map[string]string{
		"ap-northeast-1": "Asia Pacific (Tokyo)",
		"ap-northeast-2": "Asia Pacific (Seoul)",
		"ap-northeast-3": "Asia Pacific (Osaka)",
		"ap-south-1":     "Asia Pacific (Mumbai)",
		"ap-southeast-1": "Asia Pacific (Singapore)",
		"ap-southeast-2": "Asia Pacific (Sydney)",
		"ap-southeast-3": "Asia Pacific (Jakarta)",
		"ap-southeast-4": "Asia Pacific (Melbourne)",
		"ca-central-1":   "Canada (Central)",
		"eu-central-1":   "EU (Frankfurt)",
		"eu-west-1":      "EU (Ireland)",
		"eu-west-2":      "EU (London)",
		"eu-west-3":      "EU (Paris)",
		"eu-south-1":     "EU (Milan)",
		"eu-north-1":     "EU (Stockholm)",
		"eu-south-2":     "EU (Spain)",
		"eu-central-2":   "EU (Zurich)",
		"me-south-1":     "Middle East (Bahrain)",
		"me-central-1":   "Middle East (UAE)",
		"sa-east-1":      "South America (São Paulo)",
		"us-east-1":      "US East (N. Virginia)",
		"us-east-2":      "US East (Ohio)",
		"us-west-1":      "US West (N. California)",
		"us-west-2":      "US West (Oregon)",
		"af-south-1":     "Africa (Cape Town)",
		"ap-east-1":      "Asia Pacific (Hong Kong)",
		"cn-north-1":     "China (Beijing)",
		"cn-northwest-1": "China (Ningxia)",
		"il-central-1":   "Israel (Tel Aviv)",
	}
	if location, ok := regionMap[region]; ok {
		return location
	}
	// Default: use region name as is
	return region
}

// getRegionCodeFromLocation retrieves region code from Properties
func getRegionCodeFromLocation(properties []savingsplansTypes.SavingsPlanOfferingRateProperty) string {
	for _, prop := range properties {
		if prop.Name != nil && *prop.Name == "regionCode" && prop.Value != nil {
			return *prop.Value
		}
		if prop.Name != nil && *prop.Name == "location" && prop.Value != nil {
			// Reverse lookup region code from location
			return mapLocationToRegion(*prop.Value)
		}
	}
	return ""
}

// extractOnDemandPriceFromResult extracts on-demand pricing from Pricing API response
func extractOnDemandPriceFromResult(priceListEntry string) (float64, error) {
	var priceData map[string]interface{}
	err := json.Unmarshal([]byte(priceListEntry), &priceData)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal price data: %v", err)
	}

	// Get OnDemand pricing
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

// renderCSV renders CSV output for Savings Plan calculations
func renderCSV(hourlyCommitment, spPurchaseAmount, currentCost, spCost, savingsAmount, savingsRate float64, noHeader bool) {
	// Output CSV header (only if noHeader is false)
	if !noHeader {
		fmt.Println("Hourly commitment,SP/RI Purchase Amount (USD),Current Cost (USD/month),Cost After Purchase (USD/month),Savings Amount,Savings Rate")
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
