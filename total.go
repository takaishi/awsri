package awsri

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

// InstanceInfo は複数のRIを表現するための汎用的な構造体
type InstanceInfo struct {
	ServiceType  string // "rds", "elasticache" など
	InstanceType string // "m5.large" など
	Count        int    // インスタンス数
	Description  string // "postgresql", "redis" など
	MultiAz      bool   // マルチAZかどうか（RDS用）
}

// InstancePriceResult は各インスタンスの料金計算結果を表す構造体
type InstancePriceResult struct {
	ServiceType  string
	InstanceType string
	Count        int
	Upfront      float64
	Monthly      float64
	Yearly       float64
}

// TotalPriceResult は複数インスタンスの合計料金計算結果を表す構造体
type TotalPriceResult struct {
	TotalUpfront float64
	TotalMonthly float64
	TotalYearly  float64
	Instances    []InstancePriceResult
}

// TotalCommand は複数RIの合計コスト計算コマンドを表す構造体
type TotalCommand struct {
	opts TotalOption
}

// NewTotalCommand は新しいTotalCommandを作成する
func NewTotalCommand(opts TotalOption) *TotalCommand {
	return &TotalCommand{opts: opts}
}

// Run はTotalCommandを実行する
func (c *TotalCommand) Run(ctx context.Context) error {
	// インスタンス情報を解析
	instances, err := c.parseInstancesInfo()
	if err != nil {
		return fmt.Errorf("failed to parse instances info: %w", err)
	}

	// インスタンスが指定されていない場合はエラー
	if len(instances) == 0 {
		return fmt.Errorf("no instances specified")
	}

	// AWS設定を読み込み
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("ap-northeast-1"))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %w", err)
	}

	// 料金計算
	result, err := c.calculateTotalPrice(ctx, cfg, instances)
	if err != nil {
		return fmt.Errorf("failed to calculate total price: %w", err)
	}

	// 結果を表示
	c.renderResult(result)

	return nil
}

// parseInstancesInfo はコマンドライン引数からインスタンス情報を解析する
func (c *TotalCommand) parseInstancesInfo() ([]InstanceInfo, error) {
	var instances []InstanceInfo

	// RDSインスタンスの解析
	for _, rdsDef := range c.opts.RDSInstances {
		parts := strings.Split(rdsDef, ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid RDS instance format: %s, expected format: instance-type:count:product-description:multi-az", rdsDef)
		}

		instanceType := parts[0]
		count, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid count in RDS instance: %s", parts[1])
		}
		description := parts[2]
		multiAz, err := strconv.ParseBool(parts[3])
		if err != nil {
			return nil, fmt.Errorf("invalid multi-az value in RDS instance: %s", parts[3])
		}

		// RDSインスタンスタイプには "db." プレフィックスが必要
		if !strings.HasPrefix(instanceType, "db.") {
			instanceType = "db." + instanceType
		}

		instances = append(instances, InstanceInfo{
			ServiceType:  "rds",
			InstanceType: instanceType,
			Count:        count,
			Description:  description,
			MultiAz:      multiAz,
		})
	}

	// ElastiCacheインスタンスの解析
	for _, cacheDef := range c.opts.ElasticacheInstances {
		parts := strings.Split(cacheDef, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid ElastiCache instance format: %s, expected format: node-type:count:product-description", cacheDef)
		}

		instanceType := parts[0]
		count, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid count in ElastiCache instance: %s", parts[1])
		}
		description := parts[2]

		// ElastiCacheインスタンスタイプには "cache." プレフィックスが必要
		if !strings.HasPrefix(instanceType, "cache.") {
			instanceType = "cache." + instanceType
		}

		instances = append(instances, InstanceInfo{
			ServiceType:  "elasticache",
			InstanceType: instanceType,
			Count:        count,
			Description:  description,
			MultiAz:      false, // ElastiCacheはMultiAzの概念が異なる
		})
	}

	return instances, nil
}

// calculateTotalPrice は複数インスタンスの合計料金を計算する
func (c *TotalCommand) calculateTotalPrice(ctx context.Context, cfg aws.Config, instances []InstanceInfo) (TotalPriceResult, error) {
	result := TotalPriceResult{
		Instances: []InstancePriceResult{},
	}

	for _, instance := range instances {
		var upfront, monthly, yearly float64
		var err error

		switch instance.ServiceType {
		case "rds":
			upfront, monthly, yearly, err = c.calculateRDSPrice(ctx, cfg, instance)
		case "elasticache":
			upfront, monthly, yearly, err = c.calculateElastiCachePrice(ctx, cfg, instance)
		default:
			return result, fmt.Errorf("unsupported service type: %s", instance.ServiceType)
		}

		if err != nil {
			return result, err
		}

		// インスタンス数を考慮
		upfront *= float64(instance.Count)
		monthly *= float64(instance.Count)
		yearly *= float64(instance.Count)

		// 結果に追加
		result.Instances = append(result.Instances, InstancePriceResult{
			ServiceType:  instance.ServiceType,
			InstanceType: instance.InstanceType,
			Count:        instance.Count,
			Upfront:      upfront,
			Monthly:      monthly,
			Yearly:       yearly,
		})

		// 合計に加算
		result.TotalUpfront += upfront
		result.TotalMonthly += monthly
		result.TotalYearly += yearly
	}

	return result, nil
}

// calculateRDSPrice はRDSインスタンスの料金を計算する
func (c *TotalCommand) calculateRDSPrice(ctx context.Context, cfg aws.Config, instance InstanceInfo) (float64, float64, float64, error) {
	svc := rds.NewFromConfig(cfg)

	// RDSコマンドを作成して、データベースエンジンを取得
	rdsCmd := NewRDSCommand(RDSOption{
		DbInstanceClass:    instance.InstanceType,
		ProductDescription: instance.Description,
		MultiAz:            instance.MultiAz,
	})

	// データベースエンジンを取得
	databaseEngine, err := rdsCmd.getDatabaseEngine(instance.Description)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get database engine: %w", err)
	}

	// オンデマンド料金を取得（参考用）
	// エラーが発生しても処理を続行する
	_, err = rdsCmd.getRdsOnDemandPrice(cfg, instance.InstanceType, databaseEngine, instance.MultiAz)
	if err != nil {
		fmt.Printf("Warning: failed to get on-demand price for RDS %s: %v\n", instance.InstanceType, err)
	}

	// RIの料金情報を取得
	params := &rds.DescribeReservedDBInstancesOfferingsInput{
		Duration:           aws.String(strconv.Itoa(c.opts.Duration)),
		OfferingType:       aws.String(c.opts.OfferingType),
		DBInstanceClass:    aws.String(instance.InstanceType),
		ProductDescription: aws.String(instance.Description),
		MultiAZ:            aws.Bool(instance.MultiAz),
	}

	o, err := svc.DescribeReservedDBInstancesOfferings(ctx, params)
	if err != nil {
		return 0, 0, 0, err
	}

	if len(o.ReservedDBInstancesOfferings) == 0 {
		return 0, 0, 0, fmt.Errorf("no reserved instances offerings found for RDS %s with description %s and MultiAZ=%v",
			instance.InstanceType, instance.Description, instance.MultiAz)
	}

	// 適切なオファリングを取得
	offering := rdsCmd.getOffering(o.ReservedDBInstancesOfferings, instance.Description, instance.MultiAz)
	if offering == nil {
		// 利用可能なオファリングの説明を表示
		availableDescriptions := []string{}
		for _, o := range o.ReservedDBInstancesOfferings {
			desc := fmt.Sprintf("%s (MultiAZ=%v)", *o.ProductDescription, *o.MultiAZ)
			availableDescriptions = append(availableDescriptions, desc)
		}
		
		return 0, 0, 0, fmt.Errorf("no matching offering found for RDS %s with description %s and MultiAZ=%v. Available offerings: %s",
			instance.InstanceType, instance.Description, instance.MultiAz, strings.Join(availableDescriptions, ", "))
	}

	// 料金を計算
	monthlyRecurring := *offering.RecurringCharges[0].RecurringChargeAmount * 24 * 30
	fixedPrice := *offering.FixedPrice
	durationMonths := DurationToMonths(c.opts.Duration)
	effectiveYearly := CalculateEffectiveMonthly(fixedPrice, monthlyRecurring, durationMonths)

	return fixedPrice, monthlyRecurring, effectiveYearly, nil
}

// calculateElastiCachePrice はElastiCacheインスタンスの料金を計算する
func (c *TotalCommand) calculateElastiCachePrice(ctx context.Context, cfg aws.Config, instance InstanceInfo) (float64, float64, float64, error) {
	svc := elasticache.NewFromConfig(cfg)

	// ElastiCacheコマンドを作成
	elasticacheCmd := NewElastiCacheCommand(ElasticacheOption{
		CacheNodeType:      instance.InstanceType,
		ProductDescription: instance.Description,
	})

	// オンデマンド料金を取得（参考用）
	// エラーが発生しても処理を続行する
	_, err := elasticacheCmd.getElastiCacheOnDemandPrice(cfg, instance.InstanceType, instance.Description)
	if err != nil {
		fmt.Printf("Warning: failed to get on-demand price for ElastiCache %s: %v\n", instance.InstanceType, err)
	}

	// RIの料金情報を取得
	params := &elasticache.DescribeReservedCacheNodesOfferingsInput{
		Duration:           aws.String(strconv.Itoa(c.opts.Duration)),
		OfferingType:       aws.String(c.opts.OfferingType),
		CacheNodeType:      aws.String(instance.InstanceType),
		ProductDescription: aws.String(instance.Description),
	}

	o, err := svc.DescribeReservedCacheNodesOfferings(ctx, params)
	if err != nil {
		return 0, 0, 0, err
	}

	if len(o.ReservedCacheNodesOfferings) == 0 {
		return 0, 0, 0, fmt.Errorf("no reserved instances offerings found for ElastiCache %s", instance.InstanceType)
	}

	// 最初のオファリングを使用
	offering := o.ReservedCacheNodesOfferings[0]
	monthlyRecurring := *offering.RecurringCharges[0].RecurringChargeAmount * 24 * 30
	fixedPrice := *offering.FixedPrice
	durationMonths := DurationToMonths(c.opts.Duration)
	effectiveYearly := CalculateEffectiveMonthly(fixedPrice, monthlyRecurring, durationMonths)

	return fixedPrice, monthlyRecurring, effectiveYearly, nil
}

// renderResult は計算結果を表示する
func (c *TotalCommand) renderResult(result TotalPriceResult) {
	// 同じインスタンスタイプをまとめるためのマップ
	// キー: "サービスタイプ:インスタンスタイプ" (例: "rds:db.m5.large")
	// 値: まとめた結果
	groupedInstances := make(map[string]InstancePriceResult)

	// 各インスタンスの結果をグループ化
	for _, instance := range result.Instances {
		key := fmt.Sprintf("%s:%s", instance.ServiceType, instance.InstanceType)
		
		if existing, ok := groupedInstances[key]; ok {
			// 既存のエントリがある場合は値を合算
			existing.Count += instance.Count
			existing.Upfront += instance.Upfront
			existing.Monthly += instance.Monthly
			existing.Yearly += instance.Yearly
			groupedInstances[key] = existing
		} else {
			// 新しいエントリを追加
			groupedInstances[key] = instance
		}
	}

	// 出力形式に応じて表示方法を切り替え
	switch c.opts.Format {
	case "csv":
		c.renderCSV(result, groupedInstances)
	default: // "table"
		c.renderTable(result, groupedInstances)
	}
}

// renderTable はテーブル形式で結果を表示する
func (c *TotalCommand) renderTable(result TotalPriceResult, groupedInstances map[string]InstancePriceResult) {
	// テーブルレンダラーを作成
	tableRenderer := NewTableRenderer()

	// グループ化した結果を表示
	for _, instance := range groupedInstances {
		serviceName := "RDS"
		if instance.ServiceType == "elasticache" {
			serviceName = "ElastiCache"
		}

		tableRenderer.AppendReservedRow(
			c.opts.Duration,
			fmt.Sprintf("%s (%s %s x%d)", c.opts.OfferingType, serviceName, instance.InstanceType, instance.Count),
			instance.Upfront,
			instance.Monthly,
			instance.Yearly,
			0, // 節約額は表示しない
			0, // 節約率は表示しない
		)
	}

	// 区切り線を追加
	tableRenderer.AppendSeparator()

	// 合計を表示
	tableRenderer.AppendTotalRow(
		c.opts.Duration,
		"Total",
		result.TotalUpfront,
		result.TotalMonthly,
		result.TotalYearly,
	)

	// テーブルをレンダリング
	tableRenderer.Render()
}

// renderCSV はCSV形式で結果を表示する
func (c *TotalCommand) renderCSV(result TotalPriceResult, groupedInstances map[string]InstancePriceResult) {
	// CSVヘッダーを出力
	fmt.Println("Duration,OfferingType,ServiceType,InstanceType,Count,Upfront,Monthly,Yearly")

	// グループ化した結果を表示
	for _, instance := range groupedInstances {
		serviceName := "RDS"
		if instance.ServiceType == "elasticache" {
			serviceName = "ElastiCache"
		}

		fmt.Printf("%dy,%s,%s,%s,%d,%.1f,%.1f,%.1f\n",
			c.opts.Duration,
			c.opts.OfferingType,
			serviceName,
			instance.InstanceType,
			instance.Count,
			instance.Upfront,
			instance.Monthly,
			instance.Yearly,
		)
	}

	// 合計を表示
	fmt.Printf("%dy,%s,%s,%s,%s,%.1f,%.1f,%.1f\n",
		c.opts.Duration,
		"Total",
		"",
		"",
		"",
		result.TotalUpfront,
		result.TotalMonthly,
		result.TotalYearly,
	)
}