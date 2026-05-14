package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Cache is a tiny JSON-on-disk store of previous lookup hits.
//
// Why a JSON file and not SQLite:
//   For a personal project that does maybe a few hundred unique
//   barcodes a year, SQLite is overkill. A single map flushed to disk
//   on every write is fine, debuggable with `cat`, and saves us a cgo
//   dependency. If you ever scan thousands of items per day, swap this
//   for `modernc.org/sqlite` — the Source interface won't change.
//
// File format:
//   {
//     "barcodes": { "8901719100017": { ...NutritionInfo... }, ... },
//     "names":    { "dal makhani":  { ...NutritionInfo... }, ... }
//   }
type Cache struct {
	path string
	mu   sync.Mutex
	data cacheData
}

type cacheData struct {
	Barcodes map[string]*NutritionInfo `json:"barcodes"`
	Names    map[string]*NutritionInfo `json:"names"`
}

func OpenCache(path string) (*Cache, error) {
	c := &Cache{
		path: path,
		data: cacheData{
			Barcodes: map[string]*NutritionInfo{},
			Names:    map[string]*NutritionInfo{},
		},
	}
	buf, err := os.ReadFile(path)
	if err == nil {
		if jerr := json.Unmarshal(buf, &c.data); jerr != nil {
			// Corrupt cache shouldn't block the app — log and start
			// over with an empty one.
			fmt.Fprintf(defaultStderr, "warn: cache file %s unreadable, ignoring: %v\n", path, jerr)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if c.data.Barcodes == nil {
		c.data.Barcodes = map[string]*NutritionInfo{}
	}
	if c.data.Names == nil {
		c.data.Names = map[string]*NutritionInfo{}
	}
	return c, nil
}

func (c *Cache) GetBarcode(code string) *NutritionInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.data.Barcodes[code]; ok {
		copy := *v
		return &copy
	}
	return nil
}

func (c *Cache) PutBarcode(code string, info *NutritionInfo) {
	c.mu.Lock()
	c.data.Barcodes[code] = info
	c.mu.Unlock()
	c.flush()
}

func (c *Cache) GetName(query string) *NutritionInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.data.Names[strings.ToLower(query)]; ok {
		copy := *v
		return &copy
	}
	return nil
}

func (c *Cache) PutName(query string, info *NutritionInfo) {
	c.mu.Lock()
	c.data.Names[strings.ToLower(query)] = info
	c.mu.Unlock()
	c.flush()
}

func (c *Cache) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	buf, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		fmt.Fprintf(defaultStderr, "warn: cache flush marshal: %v\n", err)
		return
	}
	if err := os.WriteFile(c.path, buf, 0o644); err != nil {
		fmt.Fprintf(defaultStderr, "warn: cache flush write: %v\n", err)
	}
}
