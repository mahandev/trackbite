package main

import (
	"context"
	"errors"
	"fmt"
)

// NutritionInfo is the unified shape every data source has to return.
// We keep everything in *per-100-gram* units; the final kcal-for-the-
// weighed-food math is a single multiplication, done in main.go.
type NutritionInfo struct {
	Name           string
	Brand          string
	Source         string  // human-readable, e.g. "Open Food Facts"
	KcalPer100g    float64
	ProteinPer100g float64
	CarbsPer100g   float64
	FatPer100g     float64
}

// Source is anything that can answer a barcode or a name query.
//
// A source returning (nil, ErrNotFound) is a normal miss — the chain
// moves on to the next source. Any other error short-circuits the chain
// so the user notices misconfigured keys etc.
type Source interface {
	Name() string
	LookupBarcode(ctx context.Context, code string) (*NutritionInfo, error)
	LookupName(ctx context.Context, query string) (*NutritionInfo, error)
}

// ErrNotFound signals a source didn't have the food.
var ErrNotFound = errors.New("not found")

// Chain runs a list of sources in order and returns the first hit.
// Misses (`ErrNotFound`) are silently skipped; other errors are logged
// to stderr and treated as a miss so a broken upstream doesn't take
// down the rest of the chain.
type Chain struct {
	sources []Source
	cache   *Cache
}

func NewChain(cache *Cache, sources ...Source) *Chain {
	return &Chain{sources: sources, cache: cache}
}

func (c *Chain) Barcode(ctx context.Context, code string) (*NutritionInfo, error) {
	if hit := c.cache.GetBarcode(code); hit != nil {
		return hit, nil
	}
	for _, s := range c.sources {
		info, err := s.LookupBarcode(ctx, code)
		if errors.Is(err, ErrNotFound) || info == nil {
			continue
		}
		if err != nil {
			fmt.Fprintf(defaultStderr, "warn: %s barcode lookup failed: %v\n", s.Name(), err)
			continue
		}
		c.cache.PutBarcode(code, info)
		return info, nil
	}
	return nil, ErrNotFound
}

func (c *Chain) Name(ctx context.Context, query string) (*NutritionInfo, error) {
	if hit := c.cache.GetName(query); hit != nil {
		return hit, nil
	}
	for _, s := range c.sources {
		info, err := s.LookupName(ctx, query)
		if errors.Is(err, ErrNotFound) || info == nil {
			continue
		}
		if err != nil {
			fmt.Fprintf(defaultStderr, "warn: %s name lookup failed: %v\n", s.Name(), err)
			continue
		}
		c.cache.PutName(query, info)
		return info, nil
	}
	return nil, ErrNotFound
}

// KcalForGrams scales a 100g info card to the actual weighed grams.
// Pure function — kept here so callers don't sprinkle math around.
func KcalForGrams(info *NutritionInfo, grams float64) float64 {
	return info.KcalPer100g * grams / 100.0
}
