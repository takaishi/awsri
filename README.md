# awsri

## Instalation

```
% go install github.com/takaishi/awsri/cmd/awsri@${version}
```

## Usage

```
% awsri --service=rds --db-instance-class=db.t4g.large --product-description=mysql --multi-az=true 
| Duration (Year) |  Offering Type  | One Time Payment (USD) | Usage Charges (USD, Monthly) |
|-----------------|-----------------|------------------------|------------------------------|
|               1 | No Upfront      |                      0 |                          134 |
|               1 | Partial Upfront |                    781 |                           64 |
|               1 | All Upfront     |                   1517 |                            0 |
|               3 | No Upfront      | N/A                    | N/A                          |
|               3 | Partial Upfront |                   1630 |                           45 |
|               3 | All Upfront     |                   3192 |                            0 |

```

```
# awsri --service=elasticache --db-instance-class=db.t4g.large --product-description=redis
| Duration (Year) |  Offering Type  | One Time Payment (USD) | Usage Charges (USD, Monthly) |
|-----------------|-----------------|------------------------|------------------------------|
|               1 | No Upfront      |                      0 |                           73 |
|               1 | Partial Upfront |                   4460 |                          367 |
|               1 | All Upfront     |                  28189 |                            0 |
|               3 | No Upfront      |                      0 |                         1328 |
|               3 | Partial Upfront |                   2756 |                           76 |
|               3 | All Upfront     |                  44584 |                            0 |
```
