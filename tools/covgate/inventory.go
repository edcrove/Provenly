package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// InventoryTarget is one reviewed integration behavior or E2E journey.
type InventoryTarget struct {
	ID          string   `yaml:"id"`
	Description string   `yaml:"description"`
	Surfaces    []string `yaml:"surfaces"`
}

func loadInventory(path string) ([]InventoryTarget, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Targets []InventoryTarget `yaml:"targets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, t := range doc.Targets {
		if t.ID == "" || t.Description == "" {
			return nil, fmt.Errorf("%s: every target needs id and description", path)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("%s: duplicate target %s", path, t.ID)
		}
		seen[t.ID] = true
	}
	return doc.Targets, nil
}
