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

	// 1時間あたりのコストを計算
	// Hourly commitment = インスタンス数 × 1時間あたりのSP料金
	hourlyCommitment := float64(c.opts.Count) * spPrice

	// 購入するSP/RI (USD) = Hourly commitment × 720時間 × 12ヶ月 × 期間（年）
	hoursPerMonth := 720.0
	spPurchaseAmount := hourlyCommitment * hoursPerMonth * 12.0 * float64(c.opts.Duration)

	// 現在のコスト（オンデマンド）
	currentCostPerMonth := float64(c.opts.Count) * onDemandPrice * hoursPerMonth

	// 購入後のコスト（Savings Plan）
	spCostPerMonth := float64(c.opts.Count) * spPrice * hoursPerMonth

	// 削減コストと削減率
	savingsAmount := currentCostPerMonth - spCostPerMonth
	savingsRate := (savingsAmount / currentCostPerMonth) * 100.0

	// CSV出力
	c.renderCSV(hourlyCommitment, spPurchaseAmount, currentCostPerMonth, spCostPerMonth, savingsAmount, savingsRate, c.opts.NoHeader)

	return nil
}

// getEC2OnDemandPrice はPricing APIを使用してEC2のオンデマンド料金を取得します
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

	// 最初の結果から料金を抽出
	return c.extractEC2OnDemandPriceFromResult(result.PriceList[0])
}

// extractEC2OnDemandPriceFromResult はPricing APIのレスポンスからEC2のオンデマンド料金を抽出します
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

			// unitフィールドを確認（秒単位の場合は時間単位に変換）
			unit, _ := dimensionData["unit"].(string)

			if usdPrice, ok := pricePerUnit["USD"].(string); ok {
				price, err := strconv.ParseFloat(usdPrice, 64)
				if err != nil {
					continue
				}

				// 単位が秒の場合は時間単位に変換（秒 × 3600 = 時間）
				if strings.Contains(strings.ToLower(unit), "second") || strings.Contains(strings.ToLower(unit), "sec") {
					price = price * 3600.0
				}

				return price, nil // 1時間あたりの料金を返す
			}
		}
	}

	return 0, fmt.Errorf("price not found in pricing data")
}

// getComputeSavingsPlanPrice はSavings Plans APIを使用してEC2のSavings Plan料金を取得します
func (c *EC2Command) getComputeSavingsPlanPrice(cfg aws.Config) (float64, error) {
	svc := savingsplans.NewFromConfig(cfg)

	// 支払いオプションを引数から取得
	paymentOptionStr := c.opts.PaymentOption
	if paymentOptionStr == "" {
		paymentOptionStr = "no-upfront"
	}

	// 小文字・ハイフンつなぎの値をAWS APIが期待する形式に変換
	awsPaymentOption, err := convertPaymentOptionToAWSFormat(paymentOptionStr)
	if err != nil {
		return 0, err
	}

	paymentOption := savingsplansTypes.SavingsPlanPaymentOption(awsPaymentOption)

	// Savings Plans Offering Ratesを取得
	durationSeconds := int64(c.opts.Duration * 365 * 24 * 60 * 60) // 年数を秒に変換

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
		// 指定された支払いオプションで見つからない場合、他のオプションを試す
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

	// 期間とインスタンスタイプに一致するオファーを探す
	var matchedRate float64
	found := false

	for _, offering := range result.SearchResults {
		// 期間が一致するか確認
		if offering.SavingsPlanOffering != nil && offering.SavingsPlanOffering.DurationSeconds != durationSeconds {
			continue
		}

		// リージョンが一致するか確認
		regionCode := c.getRegionCodeFromLocation(offering.Properties)
		if regionCode != "" && regionCode != c.opts.Region {
			continue
		}

		// インスタンスタイプが一致するか確認
		instanceType := c.getInstanceTypeFromProperties(offering.Properties)
		if instanceType != "" && instanceType != c.opts.InstanceType {
			continue
		}

		// Linuxを確認（Windowsを除外）
		if offering.UsageType != nil {
			usageType := strings.ToLower(*offering.UsageType)
			if strings.Contains(usageType, "windows") {
				continue
			}
		}

		// Propertiesからも確認
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

		// Rateを取得
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

// getRegionCodeFromLocation はPropertiesからリージョンコードを取得します
func (c *EC2Command) getRegionCodeFromLocation(properties []savingsplansTypes.SavingsPlanOfferingRateProperty) string {
	for _, prop := range properties {
		if prop.Name != nil && *prop.Name == "regionCode" && prop.Value != nil {
			return *prop.Value
		}
		if prop.Name != nil && *prop.Name == "location" && prop.Value != nil {
			// locationからリージョンコードを逆引き
			return c.mapLocationToRegion(*prop.Value)
		}
	}
	return ""
}

// getInstanceTypeFromProperties はPropertiesからインスタンスタイプを取得します
func (c *EC2Command) getInstanceTypeFromProperties(properties []savingsplansTypes.SavingsPlanOfferingRateProperty) string {
	for _, prop := range properties {
		if prop.Name != nil && *prop.Name == "instanceType" && prop.Value != nil {
			return *prop.Value
		}
	}
	return ""
}

// mapLocationToRegion はlocation名からリージョンコードを取得します
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
	// リージョン名をPricing APIのlocation形式にマッピング
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
	// デフォルトはリージョン名をそのまま使用
	return region
}

func (c *EC2Command) renderCSV(hourlyCommitment, spPurchaseAmount, currentCost, spCost, savingsAmount, savingsRate float64, noHeader bool) {
	// CSVヘッダーを出力（noHeaderがfalseの場合のみ）
	if !noHeader {
		fmt.Println("Hourly commitment,購入するSP/RI (USD),現在のコスト(USD/月),購入後のコスト(USD/月),削減コスト,削減率")
	}

	// データ行を出力
	// hourly commitmentは四捨五入不要、それ以外は小数点以下不要
	fmt.Printf("%g,%.0f,%.0f,%.0f,%.0f,%.0f\n",
		hourlyCommitment,
		spPurchaseAmount,
		currentCost,
		spCost,
		savingsAmount,
		savingsRate,
	)
}
