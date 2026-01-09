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
	MemoryGBPerHour float64 `required:"" help:"Memory MB per hour (will be converted to GB)"`
	VCPUPerHour     float64 `required:"" help:"vCPU millicores per hour (will be converted to vCPU)"`
	TaskCount       int     `required:"" help:"Number of tasks"`
	Duration        int     `name:"duration" default:"1" help:"Duration in years (1 or 3)"`
	Architecture    string  `name:"architecture" default:"linux" help:"Architecture (linux or arm)"`
	PaymentOption   string  `name:"payment-option" default:"No Upfront" help:"Payment option (No Upfront, Partial Upfront, All Upfront)"`
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
	// Pricing APIとSavings Plans APIはus-east-1でのみ利用可能
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %v", err)
	}

	// オンデマンド料金を取得
	onDemandPricing, err := c.getFargateOnDemandPrice(cfg)
	if err != nil {
		return fmt.Errorf("failed to get on-demand price: %v", err)
	}

	// Savings Plan料金を取得
	spPricing, err := c.getComputeSavingsPlanPrice(cfg)
	if err != nil {
		return fmt.Errorf("failed to get Savings Plan price: %v", err)
	}

	// 入力パラメータの単位を変換
	// vCPU: millicores単位の可能性があるため、1024で割ってvCPU数に変換
	// Memory: MB単位の可能性があるため、1024で割ってGBに変換
	vcpuCount := c.opts.VCPUPerHour / 1024.0
	memoryGB := c.opts.MemoryGBPerHour / 1024.0

	// 月間コスト計算（720時間/月）
	// TaskCount × vCPU数 × 720時間 × vCPU単価 + TaskCount × GB数 × 720時間 × GB単価
	hoursPerMonth := 720.0
	currentCostPerMonth := float64(c.opts.TaskCount)*vcpuCount*hoursPerMonth*onDemandPricing.VCPUOnDemandPrice +
		float64(c.opts.TaskCount)*memoryGB*hoursPerMonth*onDemandPricing.MemoryOnDemandPrice

	spCostPerMonth := float64(c.opts.TaskCount)*vcpuCount*hoursPerMonth*spPricing.VCPUSPPrice +
		float64(c.opts.TaskCount)*memoryGB*hoursPerMonth*spPricing.MemorySPPrice

	// 1時間あたりのコストを計算（Hourly commitment用）
	hourlySPCost := spCostPerMonth / hoursPerMonth

	// Hourly commitment = Savings Plan適用後の1時間あたりのコスト
	hourlyCommitment := hourlySPCost

	// 購入するSP/RI (USD) = Hourly commitment × 720時間 × 12ヶ月 × 期間（年）
	spPurchaseAmount := hourlyCommitment * hoursPerMonth * 12.0 * float64(c.opts.Duration)

	// 削減コストと削減率
	savingsAmount := currentCostPerMonth - spCostPerMonth
	savingsRate := (savingsAmount / currentCostPerMonth) * 100.0

	// CSV出力
	c.renderCSV(hourlyCommitment, spPurchaseAmount, currentCostPerMonth, spCostPerMonth, savingsAmount, savingsRate, c.opts.NoHeader)

	return nil
}

// getFargateOnDemandPrice はPricing APIを使用してFargateのオンデマンド料金を取得します
func (c *FargateCommand) getFargateOnDemandPrice(cfg aws.Config) (*FargatePricing, error) {
	svc := pricing.NewFromConfig(cfg)
	location := c.mapRegionToLocation(c.opts.Region)

	// アーキテクチャに応じたフィルタを追加
	processorArchitecture := "x86_64"
	if c.opts.Architecture == "arm" {
		processorArchitecture = "ARM"
	}

	// vCPU料金を取得（cputype=perCPUフィルタとアーキテクチャフィルタを使用）
	vcpuPrice, err := c.getFargateOnDemandPriceByType(svc, location, "cputype", "perCPU", processorArchitecture)
	if err != nil {
		return nil, fmt.Errorf("failed to get vCPU price: %v", err)
	}

	// メモリ料金を取得（memorytype=perGBフィルタとアーキテクチャフィルタを使用）
	memoryPrice, err := c.getFargateOnDemandPriceByType(svc, location, "memorytype", "perGB", processorArchitecture)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory price: %v", err)
	}

	return &FargatePricing{
		VCPUOnDemandPrice:   vcpuPrice,
		MemoryOnDemandPrice: memoryPrice,
	}, nil
}

// getFargateOnDemandPriceByType は指定されたフィルタタイプでFargateのオンデマンド料金を取得します
func (c *FargateCommand) getFargateOnDemandPriceByType(svc *pricing.Client, location, filterType, filterValue, processorArchitecture string) (float64, error) {
	// まず、アーキテクチャフィルタなしで検索
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

	result, err := svc.GetProducts(context.TODO(), input)
	if err != nil {
		return 0, fmt.Errorf("failed to get products: %v", err)
	}

	if len(result.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing information found for %s=%s in location %s", filterType, filterValue, location)
	}

	// アーキテクチャでフィルタリング
	// Pricing APIのレスポンスでは、processorArchitecture属性が空の場合があるため、
	// usagetypeにアーキテクチャ情報が含まれている（例: APN1-Fargate-ARM-vCPU-Hours:perCPU）
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

		// 複数の属性名でアーキテクチャを確認
		arch := ""
		if val, ok := attributes["processorArchitecture"].(string); ok {
			arch = val
		} else if val, ok := attributes["ProcessorArchitecture"].(string); ok {
			arch = val
		} else if val, ok := attributes["processor"].(string); ok {
			arch = val
		}

		usagetype, _ := attributes["usagetype"].(string)

		// ARMの場合は、usagetypeに"ARM"が含まれているかも確認
		if processorArchitecture == "ARM" {
			if strings.Contains(strings.ToUpper(usagetype), "ARM") || arch == "ARM" {
				matchedPrice = priceListEntry
				break
			}
		} else if arch == processorArchitecture {
			// x86_64の場合は、ARMを含まないusagetypeを探す
			if !strings.Contains(strings.ToUpper(usagetype), "ARM") {
				matchedPrice = priceListEntry
				break
			}
		}
	}

	if matchedPrice != "" {
		return c.extractOnDemandPriceFromResult(matchedPrice)
	}

	// アーキテクチャが一致しない場合、最初の結果を使用（フォールバック）
	if len(result.PriceList) > 0 {
		return c.extractOnDemandPriceFromResult(result.PriceList[0])
	}

	return 0, fmt.Errorf("no pricing information found")
}

// extractOnDemandPriceFromResult はPricing APIのレスポンスからオンデマンド料金を抽出します
func (c *FargateCommand) extractOnDemandPriceFromResult(priceListEntry string) (float64, error) {
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

// getComputeSavingsPlanPrice はSavings Plans APIを使用してFargateのSavings Plan料金を取得します
func (c *FargateCommand) getComputeSavingsPlanPrice(cfg aws.Config) (*FargatePricing, error) {
	svc := savingsplans.NewFromConfig(cfg)

	// 支払いオプションを引数から取得
	// 有効な値: "No Upfront", "Partial Upfront", "All Upfront"
	paymentOptionStr := c.opts.PaymentOption
	// デフォルト値の設定
	if paymentOptionStr == "" {
		paymentOptionStr = "No Upfront"
	}

	// 有効な値かチェック
	validOptions := map[string]bool{
		"No Upfront":      true,
		"Partial Upfront": true,
		"All Upfront":     true,
	}
	if !validOptions[paymentOptionStr] {
		return nil, fmt.Errorf("invalid payment option: %s (must be one of: No Upfront, Partial Upfront, All Upfront)", paymentOptionStr)
	}

	paymentOption := savingsplansTypes.SavingsPlanPaymentOption(paymentOptionStr)

	// Savings Plans Offering Ratesを取得
	// リージョンフィルタを追加
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

	result, err := svc.DescribeSavingsPlansOfferingRates(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe savings plans offering rates: %v", err)
	}

	if len(result.SearchResults) == 0 {
		// 指定された支払いオプションで見つからない場合、他のオプションを試す
		// No Upfrontで見つからない場合、All Upfrontを試す
		if paymentOptionStr == "No Upfront" {
			input.SavingsPlanPaymentOptions = []savingsplansTypes.SavingsPlanPaymentOption{
				savingsplansTypes.SavingsPlanPaymentOptionAllUpfront,
			}
			result, err = svc.DescribeSavingsPlansOfferingRates(context.TODO(), input)
			if err != nil {
				return nil, fmt.Errorf("failed to describe savings plans offering rates (All Upfront): %v", err)
			}
		}
		if len(result.SearchResults) == 0 {
			return nil, fmt.Errorf("no savings plans offering rates found for payment option: %s", paymentOptionStr)
		}
	}

	// 期間に応じたオファーをフィルタリング
	durationSeconds := int64(c.opts.Duration * 365 * 24 * 60 * 60) // 年数を秒に変換

	// アーキテクチャに応じたフィルタリング条件
	isARM := c.opts.Architecture == "arm"

	var vcpuPrice, memoryPrice float64
	foundVCPU := false
	foundMemory := false

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

		// UsageTypeとRateを確認
		// まず、offering.UsageTypeを確認
		if offering.UsageType != nil {
			usageType := strings.ToLower(*offering.UsageType)

			// Windowsを除外
			if strings.Contains(usageType, "windows") {
				continue
			}

			// アーキテクチャの一致を確認
			hasARM := strings.Contains(usageType, "arm")
			if isARM && !hasARM {
				continue // ARMを指定しているが、ARMを含まないUsageTypeはスキップ
			}
			if !isARM && hasARM {
				continue // Linux x86_64を指定しているが、ARMを含むUsageTypeはスキップ
			}

			// vCPUまたはMemoryのUsageTypeを判定
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

		// PropertiesからもUsageTypeを確認
		for _, prop := range offering.Properties {
			if prop.Name != nil && prop.Value != nil {
				// UsageTypeを確認
				if *prop.Name == "usagetype" {
					usageType := strings.ToLower(*prop.Value)

					// Windowsを除外
					if strings.Contains(usageType, "windows") {
						continue
					}

					// アーキテクチャの一致を確認
					hasARM := strings.Contains(usageType, "arm")
					if isARM && !hasARM {
						continue // ARMを指定しているが、ARMを含まないUsageTypeはスキップ
					}
					if !isARM && hasARM {
						continue // Linux x86_64を指定しているが、ARMを含むUsageTypeはスキップ
					}

					// vCPUまたはMemoryのUsageTypeを判定
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

		// unitフィールドも確認
		unit := strings.ToLower(string(offering.Unit))
		if strings.Contains(unit, "hour") || strings.Contains(unit, "hr") {
			// これは時間単位の料金
			if offering.Rate != nil {
				rate, err := strconv.ParseFloat(*offering.Rate, 64)
				if err == nil {
					// UsageTypeで判定できない場合、unitとrateから推測
					// 通常、vCPUの方がメモリより高い
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

	// 見つからない場合、すべての結果を検索して最初のvCPUとMemoryを見つける
	if !foundVCPU || !foundMemory {
		for _, offering := range result.SearchResults {
			if offering.SavingsPlanOffering != nil && offering.SavingsPlanOffering.DurationSeconds != durationSeconds {
				continue
			}

			regionCode := c.getRegionCodeFromLocation(offering.Properties)
			if regionCode != "" && regionCode != c.opts.Region {
				continue
			}

			if offering.Rate != nil {
				rate, err := strconv.ParseFloat(*offering.Rate, 64)
				if err != nil {
					continue
				}

				// UsageTypeで判定
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

				// PropertiesからもUsageTypeを確認
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

				// どちらも見つからない場合、rateの値で推測
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

// getRegionCodeFromLocation はPropertiesからリージョンコードを取得します
func (c *FargateCommand) getRegionCodeFromLocation(properties []savingsplansTypes.SavingsPlanOfferingRateProperty) string {
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

// mapLocationToRegion はlocation名からリージョンコードを取得します
func (c *FargateCommand) mapLocationToRegion(location string) string {
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

func (c *FargateCommand) mapRegionToLocation(region string) string {
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

func (c *FargateCommand) renderCSV(hourlyCommitment, spPurchaseAmount, currentCost, spCost, savingsAmount, savingsRate float64, noHeader bool) {
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
