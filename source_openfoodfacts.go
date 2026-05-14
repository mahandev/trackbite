package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenFoodFacts is our primary source for branded barcoded products.
//
// Why we hit the Indian mirror (in.openfoodfacts.org) first:
//   The mirror is configured to bias `countries_tags=en:india`, which
//   means Indian-brand products surface first when there are duplicate
//   contributions. The product DB itself is the same as the global one;
//   only the prioritisation differs.
//
// API docs: https://openfoodfacts.github.io/openfoodfacts-server/api/
//
// Endpoint we use:
//   GET https://in.openfoodfacts.org/api/v2/product/{barcode}.json
//
// No API key. We do politely send a descriptive User-Agent — Open Food
// Facts asks all clients to identify themselves so they can debug
// traffic spikes and contact misbehaving clients.
type OpenFoodFacts struct {
	client *http.Client
	ua     string
}

func NewOpenFoodFacts() *OpenFoodFacts {
	return &OpenFoodFacts{
		client: &http.Client{Timeout: 10 * time.Second},
		ua:     "trackbite/0.1 (+https://github.com/mahandev/trackbite)",
	}
}

func (o *OpenFoodFacts) Name() string { return "Open Food Facts" }

// offResponse mirrors the slice of the OFF v2 product response we care
// about. The full response has hundreds of fields; we deliberately
// decode only what we need so adding sources later is one struct, not
// one giant schema.
type offResponse struct {
	Status  int `json:"status"`
	Product struct {
		ProductName string `json:"product_name"`
		Brands      string `json:"brands"`
		Nutriments  struct {
			EnergyKcal100g float64 `json:"energy-kcal_100g"`
			Proteins100g   float64 `json:"proteins_100g"`
			Carbs100g      float64 `json:"carbohydrates_100g"`
			Fat100g        float64 `json:"fat_100g"`
		} `json:"nutriments"`
	} `json:"product"`
}

func (o *OpenFoodFacts) LookupBarcode(ctx context.Context, code string) (*NutritionInfo, error) {
	// EAN-13 / UPC-A normalization: OFF stores barcodes as their full
	// 13-digit form. A 12-digit UPC-A gets a leading zero to become a
	// valid EAN-13 — same product, just zero-padded.
	if len(code) == 12 {
		code = "0" + code
	}
	url := "https://in.openfoodfacts.org/api/v2/product/" + code + ".json"
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", o.ua)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OFF returned %d", resp.StatusCode)
	}

	var body offResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode OFF response: %w", err)
	}
	if body.Status != 1 {
		return nil, ErrNotFound
	}
	if body.Product.Nutriments.EnergyKcal100g == 0 {
		// OFF sometimes has the product entry but zero calorie data
		// because volunteers filled in everything except nutrition.
		// Treat that as a miss so we fall through to the next source.
		return nil, ErrNotFound
	}

	return &NutritionInfo{
		Name:           strings.TrimSpace(body.Product.ProductName),
		Brand:          strings.TrimSpace(body.Product.Brands),
		Source:         o.Name(),
		KcalPer100g:    body.Product.Nutriments.EnergyKcal100g,
		ProteinPer100g: body.Product.Nutriments.Proteins100g,
		CarbsPer100g:   body.Product.Nutriments.Carbs100g,
		FatPer100g:     body.Product.Nutriments.Fat100g,
	}, nil
}

// OFF has a name-search endpoint too, but its quality for the kind of
// generic names we'd type ("dal makhani") is markedly worse than IFCT's
// curated table. We skip it and let IFCT handle name queries.
func (o *OpenFoodFacts) LookupName(ctx context.Context, query string) (*NutritionInfo, error) {
	return nil, ErrNotFound
}
