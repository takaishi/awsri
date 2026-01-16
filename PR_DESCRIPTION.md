# Add Compute Savings Plans Support for Fargate and EC2

## Summary

This PR introduces support for Compute Savings Plans calculations for both AWS Fargate and EC2 instances. The commands have been reorganized under a new `compute-savings-plans` subcommand structure for better organization and consistency.

## Changes

### New Features

- **Compute Savings Plans Command Structure**: Added a new `compute-savings-plans` command with subcommands:
  - `compute-savings-plans fargate`: Calculate Savings Plan costs for Fargate workloads
  - `compute-savings-plans ec2`: Calculate Savings Plan costs for EC2 instances

- **Fargate Savings Plan Support**: 
  - Calculate hourly commitment, purchase amount, and savings for Fargate tasks
  - Support for both Linux and ARM architectures
  - Configurable payment options (no-upfront, partial-upfront, all-upfront)
  - Handles vCPU and memory pricing separately

- **EC2 Savings Plan Support**:
  - Calculate hourly commitment, purchase amount, and savings for EC2 instances
  - Support for all EC2 instance types
  - Configurable payment options (no-upfront, partial-upfront, all-upfront)

### Improvements

- **Payment Option Format**: Standardized payment options to use lowercase hyphenated format (`no-upfront`, `partial-upfront`, `all-upfront`) for better CLI consistency
- **Command Name Fix**: Fixed Kong's automatic conversion of `EC2` to `ec-2` by using explicit `name` tag in struct definition
- **Code Quality**: 
  - Converted all Japanese comments to English for better maintainability
  - Updated CSV headers to English for international compatibility

### Technical Details

- Uses AWS Pricing API to fetch on-demand pricing
- Uses AWS Savings Plans API to fetch Savings Plan rates
- Handles unit conversions (millicores to vCPU, MB to GB for Fargate)
- Supports multiple regions and architectures
- Includes fallback logic when specific payment options are not available

## Usage Examples

### Fargate Savings Plan Calculation

```bash
awsri compute-savings-plans fargate \
  --memory-gb-per-hour=4096 \
  --vcpu-per-hour=2048 \
  --task-count=10 \
  --duration=1 \
  --payment-option=no-upfront \
  --region=ap-northeast-1
```

### EC2 Savings Plan Calculation

```bash
awsri compute-savings-plans ec2 \
  --instance-type=m5.large \
  --count=5 \
  --duration=1 \
  --payment-option=no-upfront \
  --region=ap-northeast-1
```

## Output Format

The commands output CSV format with the following columns:
- `Hourly commitment`: Required hourly commitment for the Savings Plan
- `SP/RI Purchase Amount (USD)`: Total purchase amount for the Savings Plan
- `Current Cost (USD/month)`: Current monthly cost with on-demand pricing
- `Cost After Purchase (USD/month)`: Monthly cost after applying Savings Plan
- `Savings Amount`: Amount saved per month
- `Savings Rate`: Percentage of savings

## Breaking Changes

- The `fargate` and `ec2` commands are now subcommands under `compute-savings-plans`
  - Old: `awsri fargate ...`
  - New: `awsri compute-savings-plans fargate ...`
  - Old: `awsri ec2 ...`
  - New: `awsri compute-savings-plans ec2 ...`

## Testing

- [x] Verified Fargate Savings Plan calculations
- [x] Verified EC2 Savings Plan calculations
- [x] Tested with different payment options
- [x] Tested with different regions
- [x] Verified CSV output format
- [x] Verified help text and command structure

## Related Issues

N/A
