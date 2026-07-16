package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalizePricingVendors(t *testing.T) {
	aliases, vendors := canonicalizePricingVendors(map[int]*Vendor{
		23: {Id: 23, Name: " openai ", Description: "duplicate description", Icon: "OpenAI"},
		5:  {Id: 5, Name: "OpenAI"},
		3:  {Id: 3, Name: "Google", Icon: "Gemini.Color"},
	})

	require.Equal(t, map[int]int{3: 3, 5: 5, 23: 5}, aliases)
	require.Equal(t, []PricingVendor{
		{ID: 3, Name: "Google", Icon: "Gemini.Color"},
		{ID: 5, Name: "OpenAI", Description: "duplicate description", Icon: "OpenAI"},
	}, vendors)
}
