package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// Variant is one (operation, declared response status) of the contract.
type Variant struct {
	Key         string // "METHOD /path"
	OperationID string
	Status      string
}

func (v Variant) ID() string { return v.OperationID + " " + v.Status }

// loadVariants derives the Contract denominator from api/openapi.yaml.
func loadVariants(path string) ([]Variant, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var out []Variant
	for p, item := range doc.Paths {
		for method, node := range item {
			if method == "parameters" {
				continue
			}
			var op struct {
				OperationID string               `yaml:"operationId"`
				Responses   map[string]yaml.Node `yaml:"responses"`
			}
			if err := node.Decode(&op); err != nil {
				return nil, err
			}
			if op.OperationID == "" {
				return nil, fmt.Errorf("%s %s has no operationId", method, p)
			}
			for status := range op.Responses {
				out = append(out, Variant{Key: upper(method) + " " + p, OperationID: op.OperationID, Status: status})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out, nil
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

var clientCall = regexp.MustCompile(`api\.(GET|POST|PUT|PATCH|DELETE)\(\s*'([^']+)'`)

// consumedOperations derives the operations the frontend calls through the generated client.
func consumedOperations(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, m := range clientCall.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]+" "+m[2]] = true
	}
	return out, nil
}
