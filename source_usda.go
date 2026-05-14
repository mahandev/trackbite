package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// USDA wraps the FoodData Central search API.
//
// API docs: https://fdc.nal.usda.gov/api-guide.html
// Endpoint we use:
//   GET https://api.nal.usda.gov/fdc/v1/foods/search?query=...&api_key=...
//
// FDC is *not* a great barcode database — coverage skews to US-branded
// products. But it's excellent for *generic* ingredient lookups (raw
// rice, oils, dairy, etc.), which is exactly the gap we use it for:
// when the user types a food name and IFCT didn't have it.
//
// Free tier: 1,000 requests/hour with the api.data.gov key.
type USDA struct {
	client *http.Client
	apiKey string
}

func NewUSDA(apiKey string) *USDA {
	return &USDA{
		client: &http.Client{Timeout: 10 * time.Second},
		apiKey: apiKey,
	}
}

func (u *USDA) Name() string { return "USDA FoodData Central" }

// LookupBarcode is technically supported by FDC (the `gtinUpc` field on
// branded foods) but it's almost all US SKUs. We don't bother — Open
// Food Facts has much better coverage for our use case. Returning
// ErrNotFound here keeps the chain consistent.
func (u *USDA) LookupBarcode(ctx context.Context, code string) (*NutritionInfo, error) {
	return nil, ErrNotFound
}

type usdaResp struct {
	Foods []struct {
		Description string `json:"description"`
		BrandOwner  string `json:"brandOwner"`
		FoodNutrients []struct {
			NutrientName string  `json:"nutrientName"`
			UnitName     string  `json:"unitName"`
			Value        float64 `json:"value"`
		} `json:"foodNutrients"`
	} `json:"foods"`
}

func (u *USDA) LookupName(ctx context.Context, query string) (*NutritionInfo, error) {
	if u.apiKey == "" {
		return nil, ErrNotFound
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("api_key", u.apiKey)
	q.Set("pageSize", "1") // we only want the top hit
	// "Foundation" + "SR Legacy" are the cleanest, most generic
	// per-100g entries. Branded data is noisier.
	q.Set("dataType", "Foundation,SR Legacy")

	endpoint := "https://api.nal.usda.gov/fdc/v1/foods/search?" + q.Encode()
	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("USDA returned %d", resp.StatusCode)
	}

	var body usdaResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode USDA response: %w", err)
	}
	if len(body.Foods) == 0 {
		return nil, ErrNotFound
	}
	top := body.Foods[0]

	// USDA nutrient values are per 100g for Foundation / SR Legacy data
	// types — that matches the unit we store in NutritionInfo. We pluck
	// the four macros we care about out of the foodNutrients array.
	info := &NutritionInfo{
		Name:   strings.TrimSpace(top.Description),
		Brand:  strings.TrimSpace(top.BrandOwner),
		Source: u.Name(),
	}
	for _, n := range top.FoodNutrients {
		switch n.NutrientName {
		case "Energy":
			if n.UnitName == "KCAL" || n.UnitName == "kcal" {
				info.KcalPer100g = n.Value
			}
		case "Protein":
			info.ProteinPer100g = n.Value
		case "Carbohydrate, by difference":
			info.CarbsPer100g = n.Value
		case "Total lipid (fat)":
			info.FatPer100g = n.Value
		}
	}
	if info.KcalPer100g == 0 {
		return nil, ErrNotFound
	}
	return info, nil
}
