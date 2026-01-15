package awsri

type ComputeSavingsPlansOption struct {
	Fargate FargateOption `cmd:"fargate" help:"Fargate Savings Plan"`
	Ec2     EC2Option     `cmd:"ec2" help:"EC2 Compute Savings Plan"`
}
