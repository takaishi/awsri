# awsri

AWS Reserved Instances and Savings Plans cost calculator

## Installation

```
% go install github.com/takaishi/awsri/cmd/awsri@${version}
```

## Usage

### RDS Reserved Instances

```
% awsri rds --db-instance-class=db.t4g.large --product-description=mysql --multi-az=true
| Duration (Year) |  Offering Type  | One Time Payment (USD) | Usage Charges (USD, Monthly) |
|-----------------|-----------------|------------------------|------------------------------|
|               1 | No Upfront      |                      0 |                          134 |
|               1 | Partial Upfront |                    781 |                           64 |
|               1 | All Upfront     |                   1517 |                            0 |
|               3 | No Upfront      | N/A                    | N/A                          |
|               3 | Partial Upfront |                   1630 |                           45 |
|               3 | All Upfront     |                   3192 |                            0 |
```

### ElastiCache Reserved Instances

```
% awsri elasticache --cache-node-type=cache.t4g.micro --product-description=redis
| Duration (Year) |  Offering Type  | One Time Payment (USD) | Usage Charges (USD, Monthly) |
|-----------------|-----------------|------------------------|------------------------------|
|               1 | No Upfront      |                      0 |                           73 |
|               1 | Partial Upfront |                   4460 |                          367 |
|               1 | All Upfront     |                  28189 |                            0 |
|               3 | No Upfront      |                      0 |                         1328 |
|               3 | Partial Upfront |                   2756 |                           76 |
|               3 | All Upfront     |                  44584 |                            0 |
```

### Compute Savings Plans

#### Fargate Savings Plan

Calculate Savings Plan costs for AWS Fargate workloads:

```
% awsri compute-savings-plans fargate \
  --vcpu-millicores-per-hour=1024 \
  --memory-mb-per-hour=1024 \
  --task-count=1 \
  --architecture=arm \
  --payment-option=all-upfront
Hourly commitment,SP/RI Purchase Amount (USD),Current Cost (USD/month),Cost After Purchase (USD/month),Savings Amount,Savings Rate
0.03369352,291,33,24,9,26
```

#### EC2 Compute Savings Plan

Calculate Savings Plan costs for EC2 instances:

```
% awsri compute-savings-plans ec2 \
  --instance-type r8g.2xlarge \
  --count 6 \
  --duration 1 \
  --payment-option=all-upfront
Hourly commitment,SP/RI Purchase Amount (USD),Current Cost (USD/month),Cost After Purchase (USD/month),Savings Amount,Savings Rate
2.37366,20508,2456,1709,747,30
```
