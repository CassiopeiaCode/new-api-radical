package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestValidateEpayResult(t *testing.T) {
	originalPID := operation_setting.EpayId
	operation_setting.EpayId = "merchant-42"
	t.Cleanup(func() { operation_setting.EpayId = originalPID })

	valid := &EpayOrderQueryResult{
		Code:       1,
		Pid:        "merchant-42",
		OutTradeNo: "order-1",
		Money:      "12.50",
	}
	if err := validateEpayResult(valid, "order-1", 12.50); err != nil {
		t.Fatalf("valid provider result rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*EpayOrderQueryResult)
	}{
		{"provider result failure", func(r *EpayOrderQueryResult) { r.Code = 0 }},
		{"wrong order", func(r *EpayOrderQueryResult) { r.OutTradeNo = "order-2" }},
		{"wrong merchant", func(r *EpayOrderQueryResult) { r.Pid = "merchant-9" }},
		{"invalid money", func(r *EpayOrderQueryResult) { r.Money = "not-a-number" }},
		{"wrong money", func(r *EpayOrderQueryResult) { r.Money = "12.52" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := *valid
			tt.mutate(&result)
			if err := validateEpayResult(&result, "order-1", 12.50); err == nil {
				t.Fatal("invalid provider result accepted")
			}
		})
	}
}
