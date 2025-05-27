package awsri

import (
	"testing"
)

func TestFormatOutput(t *testing.T) {
	cmd := NewGenerateCommand(GenerateOption{
		Duration:     1,
		OfferingType: "Partial Upfront",
	})

	instances := []InstanceInfo{
		{
			ServiceType:  "rds",
			InstanceType: "db.m5.large",
			Count:        2,
			Description:  "postgresql",
			MultiAz:      false,
		},
		{
			ServiceType:  "elasticache",
			InstanceType: "cache.m5.large",
			Count:        3,
			Description:  "redis",
			MultiAz:      false,
		},
	}

	// コマンド形式のテスト
	commandOutput, err := cmd.formatOutput(instances, "command")
	if err != nil {
		t.Fatalf("Failed to format command output: %v", err)
	}
	expectedCommand := `awsri total --rds=m5.large:2:postgresql:false --elasticache=m5.large:3:redis --duration=1 --offering-type="Partial Upfront"`
	if commandOutput != expectedCommand {
		t.Errorf("Command output mismatch.\nExpected: %s\nGot: %s", expectedCommand, commandOutput)
	}

	// 引数のみの形式のテスト
	argsOutput, err := cmd.formatOutput(instances, "args")
	if err != nil {
		t.Fatalf("Failed to format args output: %v", err)
	}
	expectedArgs := `--rds=m5.large:2:postgresql:false --elasticache=m5.large:3:redis`
	if argsOutput != expectedArgs {
		t.Errorf("Args output mismatch.\nExpected: %s\nGot: %s", expectedArgs, argsOutput)
	}

	// JSON形式のテスト
	jsonOutput, err := cmd.formatOutput(instances, "json")
	if err != nil {
		t.Fatalf("Failed to format JSON output: %v", err)
	}
	if len(jsonOutput) == 0 {
		t.Error("JSON output is empty")
	}
}