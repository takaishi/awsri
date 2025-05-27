# ADR 0002: AWSアカウントからtotalコマンド引数生成機能の実装

## ステータス

提案中 (2025-05-27)

## コンテキスト

現在のawsriツールでは、`total` コマンドを使用して複数のRIの合計コストを計算できますが、引数を手動で指定する必要があります。一方、`list_aws_instances.sh` スクリプトは現在のAWSアカウントのインスタンス情報を取得できますが、この情報を `total` コマンドの引数形式に変換する機能はありません。

ユーザーからの要望として、AWSアカウントから取得したインスタンス情報を自動的に `total` コマンドの引数形式に変換する機能が求められています。

## 決定事項

### 1. 新しいコマンド「generate」の追加

AWSアカウントから `total` コマンドの引数を生成するための新しいコマンド「generate」を追加します。

```
awsri generate [オプション]
```

### 2. 実装方式

#### 検討した選択肢:

1. **詳細情報を自動取得する方法**
   - 概要: AWSのAPIを使って、各インスタンスの詳細情報（エンジンタイプ、マルチAZ設定など）を取得し、完全な引数を生成する
   - メリット: ユーザーの入力が最小限で済む、正確な情報を自動的に取得できる
   - デメリット: 実装が複雑になる、APIコールが増えるため実行時間が長くなる可能性がある

2. **基本情報のみ取得し、詳細はデフォルト値または引数で指定**
   - 概要: インスタンスタイプと数だけを取得し、エンジンタイプやマルチAZ設定はデフォルト値を使用するか、コマンドライン引数で指定できるようにする
   - メリット: 実装がシンプル、実行が速い
   - デメリット: デフォルト値が実際の環境と異なる場合、正確な計算ができない、ユーザーが追加の引数を指定する必要がある場合がある

3. **対話式インターフェース**
   - 概要: 基本情報を取得した後、不足している情報（エンジンタイプ、マルチAZ設定など）をユーザーに対話的に確認する
   - メリット: ユーザーフレンドリー、正確な情報を入力できる
   - デメリット: 実装が複雑になる、自動化しにくい

#### 決定: 方針1（詳細情報を自動取得する方法）を採用

**決定理由:**
ユーザーの利便性を最大化するため、可能な限り詳細情報を自動取得する方針を採用します。これにより、ユーザーは追加の情報を入力する必要がなく、正確な引数を生成できます。実装の複雑さや実行時間の増加というデメリットはありますが、ユーザー体験の向上というメリットの方が大きいと判断しました。

ただし、一部の情報（特にRDSのproduct-descriptionやElastiCacheのproduct-description）は自動取得が難しい場合があるため、これらについてはデフォルト値を設定し、必要に応じてコマンドライン引数でオーバーライドできるようにします。

### 3. コマンドラインオプション

```
awsri generate [--region=REGION] [--rds-engine=ENGINE] [--elasticache-engine=ENGINE] [--duration=DURATION] [--offering-type=TYPE] [--output=FORMAT]
```

- `--region`: AWSリージョン（デフォルト: ap-northeast-1）
- `--rds-engine`: RDSのエンジンタイプ（デフォルト: postgresql）
- `--elasticache-engine`: ElastiCacheのエンジンタイプ（デフォルト: redis）
- `--duration`: RIの期間（デフォルト: 1）
- `--offering-type`: RIのオファリングタイプ（デフォルト: "Partial Upfront"）
- `--output`: 出力形式（options: command, args, json, デフォルト: command）

### 4. 出力形式

#### 検討した選択肢:

1. **コマンド形式**
   - 例: `awsri total --rds=m5.large:1:postgresql:false --elasticache=m5.large:2:redis --duration=1 --offering-type="Partial Upfront"`
   - メリット: そのままコピー＆ペーストで実行できる

2. **引数のみ**
   - 例: `--rds=m5.large:1:postgresql:false --elasticache=m5.large:2:redis`
   - メリット: 他のオプションと組み合わせやすい

3. **JSON形式**
   - 例: `{"rds": [{"type": "m5.large", "count": 1, ...}], ...}`
   - メリット: プログラムでの処理がしやすい

#### 決定: 複数の出力形式をサポート

**決定理由:**
異なるユースケースに対応するため、複数の出力形式をサポートします。デフォルトはコマンド形式とし、ユーザーが必要に応じて他の形式を選択できるようにします。これにより、直接実行したいユーザー、スクリプトで処理したいユーザー、他のツールと連携したいユーザーなど、様々なニーズに対応できます。

## 結果

この決定により、以下の結果が期待されます：

1. ユーザーは現在のAWSアカウントのインスタンス情報から、簡単に `total` コマンドの引数を生成できるようになります
2. 可能な限り詳細情報を自動取得することで、ユーザーの入力を最小限に抑えます
3. 複数の出力形式をサポートすることで、様々なユースケースに対応できます
4. コマンドライン引数でデフォルト値をオーバーライドできるため、柔軟性も確保されます

## 実装計画

1. `cli.go`に`GenerateOption`構造体と`GenerateCommand`構造体を追加
2. `generate.go`ファイルを新規作成し、引数生成ロジックを実装
3. AWSのAPIを使って詳細情報を取得する機能を実装
   - RDSの場合: インスタンスタイプ、数、エンジンタイプ、マルチAZ設定を取得
   - ElastiCacheの場合: ノードタイプ、数、エンジンタイプを取得
4. 複数の出力形式（command, args, json）をサポートする機能を実装
5. 既存の`list_aws_instances.sh`のロジックをGoコードに移植
6. テストを実装して機能を検証

## コード構造

```mermaid
classDiagram
    class CLI {
        +RDS: RDSOption
        +Elasticache: ElasticacheOption
        +Total: TotalOption
        +Generate: GenerateOption
        +Version: struct{}
    }
    
    class GenerateOption {
        +Region: string
        +RDSEngine: string
        +ElastiCacheEngine: string
        +Duration: int
        +OfferingType: string
        +Output: string
    }
    
    class GenerateCommand {
        -opts: GenerateOption
        +Run(ctx context.Context): error
        -getInstancesInfo(ctx context.Context, cfg aws.Config): ([]InstanceInfo, error)
        -generateTotalArgs(instances []InstanceInfo): string
        -formatOutput(instances []InstanceInfo, format string): string
    }
    
    class InstanceInfo {
        +ServiceType: string
        +InstanceType: string
        +Count: int
        +Description: string
        +MultiAz: bool
    }
    
    CLI --> GenerateOption
    GenerateOption --> GenerateCommand
    GenerateCommand --> InstanceInfo
```

## コマンド使用例

```bash
# 基本的な使用方法
awsri generate

# リージョンを指定
awsri generate --region=us-west-2

# RDSエンジンを指定
awsri generate --rds-engine=mysql

# 引数のみの出力
awsri generate --output=args

# JSON形式で出力
awsri generate --output=json

# 期間と支払いタイプを指定
awsri generate --duration=3 --offering-type="All Upfront"
```

## 将来の拡張性

1. 他のAWSサービス（EC2、Redshift等）のサポート追加
2. より詳細な情報を自動取得する機能の強化
3. 対話式インターフェースのオプション追加
4. 複数のAWSアカウントやリージョンにまたがる情報の集約