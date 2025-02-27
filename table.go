package awsri

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/olekukonko/tablewriter"
)

// Common table headers
var HEADINGS = []string{
	"Duration",
	"Offering Type",
	"Upfront (USD)",
	"Monthly (USD)",
	"Effective Monthly (USD)",
	"Savings/Month",
}

// Common offering types and durations
var OfferingTypes = []string{"On-Demand", "No Upfront", "Partial Upfront", "All Upfront"}
var Durations = []int{1, 3}

// TableRenderer handles the common table rendering functionality
type TableRenderer struct {
	table *tablewriter.Table
}

// NewTableRenderer creates a new TableRenderer
func NewTableRenderer() *TableRenderer {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(HEADINGS)
	table.SetAutoFormatHeaders(false)
	table.SetAutoWrapText(false)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")

	return &TableRenderer{
		table: table,
	}
}

// AppendOnDemandRow adds an on-demand row to the table
func (t *TableRenderer) AppendOnDemandRow(duration int, onDemandPrice float64) {
	t.table.Append([]string{
		fmt.Sprintf("%dy", duration),
		"On-Demand",
		"0",
		fmt.Sprintf("%.0f", onDemandPrice),
		fmt.Sprintf("%.0f", onDemandPrice),
		"-",
	})
}

// AppendReservedRow adds a reserved instance row to the table
func (t *TableRenderer) AppendReservedRow(
	duration int,
	offeringType string,
	fixedPrice float64,
	monthlyRecurring float64,
	effectiveMonthly float64,
	monthlySavings float64,
	savingsPercent float64,
) {
	t.table.Append([]string{
		fmt.Sprintf("%dy", duration),
		offeringType,
		fmt.Sprintf("%.0f", fixedPrice),
		fmt.Sprintf("%.0f", monthlyRecurring),
		fmt.Sprintf("%.0f", effectiveMonthly),
		fmt.Sprintf("%.0f (%.1f%%)", monthlySavings, savingsPercent),
	})
}

// AppendNotAvailableRow adds a row with N/A values
func (t *TableRenderer) AppendNotAvailableRow(duration int, offeringType string) {
	t.table.Append([]string{
		fmt.Sprintf("%dy", duration),
		offeringType,
		"N/A", "N/A", "N/A", "N/A",
	})
}

// AppendSeparator adds a separator row
func (t *TableRenderer) AppendSeparator() {
	t.table.Append([]string{"", "", "", "", "", ""})
}

// Render renders the table
func (t *TableRenderer) Render() {
	t.table.Render()
}

// PricingData represents common pricing data
type PricingData struct {
	FixedPrice       float64
	RecurringCharge  float64
	DurationMonths   int
	EffectiveMonthly float64
}

// CalculateEffectiveMonthly calculates the effective monthly cost
func CalculateEffectiveMonthly(fixedPrice float64, monthlyRecurring float64, durationMonths int) float64 {
	monthlyUpfront := fixedPrice / float64(durationMonths)
	return monthlyUpfront + monthlyRecurring
}

// CalculateSavings calculates the savings amount and percentage
func CalculateSavings(onDemandPrice float64, effectiveMonthly float64) (float64, float64) {
	monthlySavings := onDemandPrice - effectiveMonthly
	savingsPercent := (monthlySavings / onDemandPrice) * 100
	return monthlySavings, savingsPercent
}

// DurationToMonths converts years to months
func DurationToMonths(years int) int {
	return years * 12
}

// FormatDuration formats the duration as a string
func FormatDuration(years int) string {
	return strconv.Itoa(years)
}

// extractPriceFromResult extracts the price from the pricing API result
func extractPriceFromResult(result *pricing.GetProductsOutput) (float64, error) {
	if len(result.PriceList) > 0 {
		// Parse JSON response to get the price
		var priceData map[string]interface{}
		err := json.Unmarshal([]byte(result.PriceList[0]), &priceData)
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