package models

import (
	"encoding/json"
	"testing"
)

// realistic Conway-era /protocol-parameters payload, trimmed to the fields
// under test plus enough neighbours to catch a misplaced tag.
const protocolParamsBody = `{
	"data": {
		"collateral_percentage": 150,
		"max_collateral_inputs": 3,
		"max_transaction_size": {"bytes": 16384},
		"max_value_size": {"bytes": 5000},
		"max_reference_scripts_size": {"bytes": 204800},
		"min_fee_coefficient": 44,
		"min_fee_constant": {"ada": {"lovelace": 155381}},
		"min_fee_reference_scripts": {
			"base": 15,
			"range": 25600,
			"multiplier": 1.2
		},
		"min_utxo_deposit_coefficient": 4310,
		"script_execution_prices": {
			"memory": "577/10000",
			"steps": "721/10000000"
		},
		"stake_credential_deposit": {"ada": {"lovelace": 2000000}},
		"version": {"major": 10, "minor": 0}
	}
}`

// TestProtocolParamsDecodesReferenceScriptPricing is the regression guard for
// the Conway reference-script fee parameters. Without these fields a consumer
// cannot price a transaction that references a script, because the ledger
// charges a tiered per-byte fee that has no safe default.
func TestProtocolParamsDecodesReferenceScriptPricing(t *testing.T) {
	var resp struct {
		Data ProtocolParams `json:"data"`
	}
	if err := json.Unmarshal([]byte(protocolParamsBody), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	pp := resp.Data

	if got, want := pp.MinFeeReferenceScripts.Base, 15.0; got != want {
		t.Errorf("MinFeeReferenceScripts.Base = %v, want %v", got, want)
	}
	if got, want := pp.MinFeeReferenceScripts.Range, int64(25600); got != want {
		t.Errorf("MinFeeReferenceScripts.Range = %v, want %v", got, want)
	}
	if got, want := pp.MinFeeReferenceScripts.Multiplier, 1.2; got != want {
		t.Errorf("MinFeeReferenceScripts.Multiplier = %v, want %v", got, want)
	}
	if got, want := pp.MaxReferenceScriptsSize.Bytes, int64(204800); got != want {
		t.Errorf("MaxReferenceScriptsSize.Bytes = %v, want %v", got, want)
	}
}

// TestProtocolParamsFeeCriticalFieldsAreNonZero catches a whole class of silent
// failure: an unmapped or mis-tagged field decodes to zero, and a zero fee
// parameter produces an underpriced, unsubmittable transaction rather than an
// error.
func TestProtocolParamsFeeCriticalFieldsAreNonZero(t *testing.T) {
	var resp struct {
		Data ProtocolParams `json:"data"`
	}
	if err := json.Unmarshal([]byte(protocolParamsBody), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	pp := resp.Data

	checks := []struct {
		name string
		zero bool
	}{
		{"CollateralPercentage", pp.CollateralPercentage == 0},
		{"MaxCollateralInputs", pp.MaxCollateralInputs == 0},
		{"MaxTransactionSize.Bytes", pp.MaxTransactionSize.Bytes == 0},
		{"MaxValueSize.Bytes", pp.MaxValueSize.Bytes == 0},
		{"MaxReferenceScriptsSize.Bytes", pp.MaxReferenceScriptsSize.Bytes == 0},
		{"MinFeeCoefficient", pp.MinFeeCoefficient == 0},
		{
			"MinFeeConstant.LovelaceAmount.Lovelace",
			pp.MinFeeConstant.LovelaceAmount.Lovelace == 0,
		},
		{"MinFeeReferenceScripts.Base", pp.MinFeeReferenceScripts.Base == 0},
		{"MinFeeReferenceScripts.Range", pp.MinFeeReferenceScripts.Range == 0},
		{
			"MinFeeReferenceScripts.Multiplier",
			pp.MinFeeReferenceScripts.Multiplier == 0,
		},
		{"MinUtxoDepositCoefficient", pp.MinUtxoDepositCoefficient == 0},
		{"ScriptExecutionPrices.Memory", pp.ScriptExecutionPrices.Memory == ""},
		{"ScriptExecutionPrices.Steps", pp.ScriptExecutionPrices.Steps == ""},
		{"ProtocolVersion.Major", pp.ProtocolVersion.Major == 0},
	}
	for _, c := range checks {
		if c.zero {
			t.Errorf(
				"%s decoded to its zero value despite being present in the payload",
				c.name,
			)
		}
	}
}
