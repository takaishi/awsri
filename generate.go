package awsri

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

// GenerateCommand は引数生成コマンドを表す構造体
type GenerateCommand struct {
	opts GenerateOption
}

// NewGenerateCommand は新しいGenerateCommandを作成する
func NewGenerateCommand(opts GenerateOption) *GenerateCommand {
	return &GenerateCommand{opts: opts}
}

// Run はGenerateCommandを実行する
func (c *GenerateCommand) Run(ctx context.Context) error {
	// AWS設定を読み込み
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(c.opts.Region))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %w", err)
	}

	// インスタンス情報を取得
	instances, err := c.getInstancesInfo(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get instances info: %w", err)
	}

	// 出力形式に応じて結果を表示
	output, err := c.formatOutput(instances, c.opts.Output)
	if err != nil {
		return fmt.Errorf("failed to format output: %w", err)
	}

	fmt.Println(output)
	return nil
}

// getInstancesInfo はAWSアカウントからインスタンス情報を取得する
func (c *GenerateCommand) getInstancesInfo(ctx context.Context, cfg aws.Config) ([]InstanceInfo, error) {
	var instances []InstanceInfo

	// RDSインスタンス情報を取得
	rdsInstances, err := c.getRDSInstances(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get RDS instances: %w", err)
	}
	instances = append(instances, rdsInstances...)

	// ElastiCacheインスタンス情報を取得
	elasticacheInstances, err := c.getElastiCacheInstances(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get ElastiCache instances: %w", err)
	}
	instances = append(instances, elasticacheInstances...)

	return instances, nil
}

// getRDSInstances はRDSインスタンス情報を取得する
func (c *GenerateCommand) getRDSInstances(ctx context.Context, cfg aws.Config) ([]InstanceInfo, error) {
	svc := rds.NewFromConfig(cfg)
	result, err := svc.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{})
	if err != nil {
		return nil, err
	}

	// インスタンスタイプごとにカウント
	instanceCounts := make(map[string]int)
	instanceEngines := make(map[string]string)
	instanceMultiAZs := make(map[string]bool)

	for _, instance := range result.DBInstances {
		instanceType := *instance.DBInstanceClass
		instanceCounts[instanceType]++
		
		// エンジンタイプを取得
		if instance.Engine != nil {
			instanceEngines[instanceType] = *instance.Engine
		}
		
		// マルチAZ設定を取得
		instanceMultiAZs[instanceType] = *instance.MultiAZ
	}

	// InstanceInfo構造体に変換
	var instances []InstanceInfo
	for instanceType, count := range instanceCounts {
		// エンジンタイプを取得（なければデフォルト値を使用）
		engine, ok := instanceEngines[instanceType]
		if !ok {
			engine = c.opts.RDSEngine
		}
		
		// マルチAZ設定を取得
		multiAZ, ok := instanceMultiAZs[instanceType]
		if !ok {
			multiAZ = false
		}

		instances = append(instances, InstanceInfo{
			ServiceType:  "rds",
			InstanceType: instanceType,
			Count:        count,
			Description:  engine,
			MultiAz:      multiAZ,
		})
	}

	return instances, nil
}

// getElastiCacheInstances はElastiCacheインスタンス情報を取得する
func (c *GenerateCommand) getElastiCacheInstances(ctx context.Context, cfg aws.Config) ([]InstanceInfo, error) {
	svc := elasticache.NewFromConfig(cfg)
	result, err := svc.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{})
	if err != nil {
		return nil, err
	}

	// インスタンスタイプごとにカウント
	instanceCounts := make(map[string]int)
	instanceEngines := make(map[string]string)

	for _, cluster := range result.CacheClusters {
		instanceType := *cluster.CacheNodeType
		instanceCounts[instanceType]++
		
		// エンジンタイプを取得
		if cluster.Engine != nil {
			instanceEngines[instanceType] = *cluster.Engine
		}
	}

	// InstanceInfo構造体に変換
	var instances []InstanceInfo
	for instanceType, count := range instanceCounts {
		// エンジンタイプを取得（なければデフォルト値を使用）
		engine, ok := instanceEngines[instanceType]
		if !ok {
			engine = c.opts.ElastiCacheEngine
		}

		instances = append(instances, InstanceInfo{
			ServiceType:  "elasticache",
			InstanceType: instanceType,
			Count:        count,
			Description:  engine,
			MultiAz:      false, // ElastiCacheはMultiAzの概念が異なる
		})
	}

	return instances, nil
}

// formatOutput は指定された形式で出力を生成する
func (c *GenerateCommand) formatOutput(instances []InstanceInfo, format string) (string, error) {
	switch format {
	case "command":
		return c.formatCommandOutput(instances), nil
	case "args":
		return c.formatArgsOutput(instances), nil
	case "json":
		return c.formatJSONOutput(instances)
	default:
		return "", fmt.Errorf("unsupported output format: %s", format)
	}
}

// formatCommandOutput はコマンド形式で出力を生成する
func (c *GenerateCommand) formatCommandOutput(instances []InstanceInfo) string {
	args := c.formatArgsOutput(instances)
	return fmt.Sprintf("awsri total %s --duration=%d --offering-type=\"%s\"", 
		args, c.opts.Duration, c.opts.OfferingType)
}

// formatArgsOutput は引数のみの形式で出力を生成する
func (c *GenerateCommand) formatArgsOutput(instances []InstanceInfo) string {
	var rdsArgs []string
	var elasticacheArgs []string

	for _, instance := range instances {
		switch instance.ServiceType {
		case "rds":
			// RDSインスタンスの引数形式: instance-type:count:product-description:multi-az
			// db.プレフィックスを削除
			instanceType := strings.TrimPrefix(instance.InstanceType, "db.")
			rdsArgs = append(rdsArgs, fmt.Sprintf("--rds=%s:%d:%s:%t", 
				instanceType, instance.Count, instance.Description, instance.MultiAz))
		case "elasticache":
			// ElastiCacheインスタンスの引数形式: node-type:count:product-description
			// cache.プレフィックスを削除
			instanceType := strings.TrimPrefix(instance.InstanceType, "cache.")
			elasticacheArgs = append(elasticacheArgs, fmt.Sprintf("--elasticache=%s:%d:%s", 
				instanceType, instance.Count, instance.Description))
		}
	}

	return strings.Join(append(rdsArgs, elasticacheArgs...), " ")
}

// formatJSONOutput はJSON形式で出力を生成する
func (c *GenerateCommand) formatJSONOutput(instances []InstanceInfo) (string, error) {
	// JSON出力用の構造体
	type OutputInstance struct {
		ServiceType  string `json:"service_type"`
		InstanceType string `json:"instance_type"`
		Count        int    `json:"count"`
		Description  string `json:"description"`
		MultiAz      bool   `json:"multi_az,omitempty"`
	}

	type OutputData struct {
		Instances    []OutputInstance `json:"instances"`
		Duration     int              `json:"duration"`
		OfferingType string           `json:"offering_type"`
	}

	// 出力データを作成
	outputData := OutputData{
		Duration:     c.opts.Duration,
		OfferingType: c.opts.OfferingType,
		Instances:    make([]OutputInstance, 0, len(instances)),
	}

	for _, instance := range instances {
		// プレフィックスを削除
		instanceType := instance.InstanceType
		if instance.ServiceType == "rds" {
			instanceType = strings.TrimPrefix(instanceType, "db.")
		} else if instance.ServiceType == "elasticache" {
			instanceType = strings.TrimPrefix(instanceType, "cache.")
		}

		outputData.Instances = append(outputData.Instances, OutputInstance{
			ServiceType:  instance.ServiceType,
			InstanceType: instanceType,
			Count:        instance.Count,
			Description:  instance.Description,
			MultiAz:      instance.MultiAz,
		})
	}

	// JSONに変換
	jsonData, err := json.MarshalIndent(outputData, "", "  ")
	if err != nil {
		return "", err
	}

	return string(jsonData), nil
}