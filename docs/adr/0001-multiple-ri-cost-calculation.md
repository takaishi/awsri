# ADR 0001: 複数RIの合計コスト計算機能の実装

## ステータス

提案中 (2025-05-27)

## コンテキスト

現在のawsriツールでは、RDSとElastiCacheのリザーブドインスタンス（RI）の料金情報を個別に取得して表示する機能が実装されています。しかし、複数のRIを組み合わせて購入する場合の合計コストを計算する機能はありません。

ユーザーからの要望として、「rds-m5.large-1台とelasticache-m5.large-2台の合計コスト」のように、複数のRIを一度に指定して合計コストを計算できる機能が求められています。

## 決定事項

### 1. 新しいコマンド「total」の追加

複数のRIの合計コストを計算するための新しいコマンド「total」を追加します。

```
awsri total [オプション]
```

### 2. 複数RIの指定方法

#### 決定: コマンドライン引数による指定方式を採用

**検討した選択肢:**

1. **コマンドライン引数による指定**
   - 例: `awsri total --rds=m5.large:1:postgresql:false --elasticache=m5.large:2:redis --duration=1 --offering-type="Partial Upfront"`
   - メリット: シンプルなコマンドで指定できる
   - デメリット: 複雑な組み合わせの場合、コマンドが長くなる

2. **JSONファイルによる指定**
   - 例: `awsri total --file=instances.json`
   - メリット: 複雑な構成も指定しやすい、再利用性が高い
   - デメリット: ファイル作成の手間がかかる

3. **対話式インターフェース**
   - 例: `awsri total --interactive`
   - メリット: ユーザーフレンドリー
   - デメリット: 実装が複雑になる、自動化しにくい

**決定理由:**
コマンドライン引数による指定方式は、シンプルな使用ケースでは十分な機能を提供でき、実装も比較的容易です。また、既存のコマンド構造との一貫性も保てます。将来的に必要に応じてJSONファイルによる指定も追加することができます。

### 3. データ構造

#### 決定: 汎用的なインスタンス構造体を採用

**検討した選択肢:**

1. **サービスごとの配列**
   - 例: `rdsInstances[]`, `elasticacheInstances[]`
   - メリット: サービスごとに処理を分けやすい
   - デメリット: 新しいサービスを追加する際に構造変更が必要

2. **汎用的なインスタンス構造体**
   - 例: `instances[]` (各要素にサービス種別を含む)
   - メリット: 拡張性が高い、共通処理を書きやすい
   - デメリット: 型安全性が低下する可能性がある

**決定理由:**
汎用的なインスタンス構造体を採用することで、将来的に新しいAWSサービスを追加する際の拡張性が高まります。また、共通の処理ロジックを実装しやすくなります。

```go
type InstanceInfo struct {
    ServiceType string  // "rds", "elasticache" など
    InstanceType string // "m5.large" など
    Count int           // インスタンス数
    Description string  // "postgresql", "redis" など
    MultiAz bool        // マルチAZかどうか（RDS用）
}
```

### 4. 料金計算方法

#### 決定: 既存の料金取得ロジックを再利用

**検討した選択肢:**

1. **既存の料金取得ロジックの再利用**
   - メリット: コードの重複を避けられる
   - デメリット: 既存コードの修正が必要になる可能性がある

2. **新しい料金計算ロジックの実装**
   - メリット: 既存コードに影響を与えない
   - デメリット: コードの重複が発生する

**決定理由:**
既存の料金取得ロジックを再利用することで、コードの重複を避け、メンテナンス性を高めることができます。ただし、既存のコードを一部リファクタリングして、再利用しやすい構造にする必要があります。

## 結果

この決定により、以下の結果が期待されます：

1. ユーザーは複数のRIを一度に指定して、合計コストを計算できるようになります
2. コマンドライン引数による指定方式により、シンプルな使用が可能になります
3. 汎用的なデータ構造により、将来的な拡張性が確保されます
4. 既存コードの再利用により、メンテナンス性が向上します

## 実装計画

1. `cli.go`に`TotalOption`構造体と`TotalCommand`構造体を追加
2. `total.go`ファイルを新規作成し、複数インスタンスの料金計算ロジックを実装
3. 既存の`rds.go`と`elasticache.go`から料金取得ロジックを抽出して再利用可能にする
4. `table.go`に合計コスト表示用の関数を追加
5. テストを実装して機能を検証

## コード構造

```mermaid
classDiagram
    class CLI {
        +RDS: RDSOption
        +Elasticache: ElasticacheOption
        +Total: TotalOption
        +Version: struct{}
    }
    
    class TotalOption {
        +RDSInstances: []string
        +ElasticacheInstances: []string
        +Duration: int
        +OfferingType: string
    }
    
    class TotalCommand {
        -opts: TotalOption
        +Run(ctx context.Context): error
    }
    
    class InstanceInfo {
        +ServiceType: string
        +InstanceType: string
        +Count: int
        +Description: string
        +MultiAz: bool
    }
    
    class PricingCalculator {
        +CalculateTotalPrice(instances []InstanceInfo): TotalPriceResult
    }
    
    class TotalPriceResult {
        +TotalUpfront: float64
        +TotalMonthly: float64
        +TotalYearly: float64
        +Instances: []InstancePriceResult
    }
    
    class InstancePriceResult {
        +ServiceType: string
        +InstanceType: string
        +Count: int
        +Upfront: float64
        +Monthly: float64
        +Yearly: float64
    }
    
    CLI --> TotalOption
    TotalOption --> TotalCommand
    TotalCommand --> InstanceInfo
    TotalCommand --> PricingCalculator
    PricingCalculator --> TotalPriceResult
    TotalPriceResult --> InstancePriceResult
```

## コマンド使用例

```
# RDS m5.large 1台とElastiCache m5.large 2台の合計コスト（1年間、部分前払い）
awsri total --rds=m5.large:1:postgresql:false --elasticache=m5.large:2:redis --duration=1 --offering-type="Partial Upfront"

# 複数のRDSインスタンス（異なるタイプ）の合計コスト
awsri total --rds=m5.large:2:postgresql:false --rds=r5.large:1:postgresql:true --duration=3 --offering-type="All Upfront"
```

## 将来の拡張性

1. JSONファイルからの読み込み機能の追加
2. 他のAWSサービス（EC2、Redshift等）のサポート追加
3. 複数のリージョンにまたがるRIの計算機能