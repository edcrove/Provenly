package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const backendModule = "github.com/edcrove/provenly/backend"

// Paths of the evidence each gate reads, relative to the repository root.
var paths = struct {
	Spec, Exceptions, Inventories                          string
	BackendUnitProfile, BackendIntProfile, BackendIntJSON  string
	BackendContract, E2EResults                            string
	FrontendUnit, FrontendIntJSON, FrontendIntCoverage     string
	FrontendContract, FrontendE2ECoverage, FrontendQueries string
	GoCovDirs                                              map[string]string
}{
	Spec:                "api/openapi.yaml",
	Exceptions:          "coverage/exceptions.yaml",
	Inventories:         "coverage/inventories",
	BackendUnitProfile:  "coverage/out/backend-unit.out",
	BackendIntProfile:   "coverage/out/backend-integration.out",
	BackendIntJSON:      "coverage/out/backend-integration.json",
	BackendContract:     "coverage/out/backend-contract.json",
	E2EResults:          "e2e/coverage/results.json",
	FrontendUnit:        "frontend/coverage/unit/coverage-final.json",
	FrontendIntJSON:     "frontend/coverage/integration/results.json",
	FrontendIntCoverage: "frontend/coverage/integration/coverage-final.json",
	FrontendContract:    "frontend/coverage/contract/evidence.json",
	FrontendE2ECoverage: "e2e/coverage/frontend-remapped",
	FrontendQueries:     "frontend/src/api/queries.ts",
	GoCovDirs: map[string]string{
		"unit":        "coverage/out/gocov/unit",
		"integration": "coverage/out/gocov/integration",
		"contract":    "coverage/out/gocov/contract",
		"e2e":         "e2e/coverage/backend",
	},
}

type gateFunc func(root string, r *GateResult) []Element

type gateDef struct {
	name, side, layer, denominator string
	build                          gateFunc
}

var gates = []gateDef{
	{"backend-unit", "backend", "unit", "Go statements (go test -cover); branches: complementary inventory", backendUnit},
	{"backend-integration", "backend", "integration", "integration surfaces: every sqlc query + reviewed behaviors", backendIntegration},
	{"backend-contract", "backend", "contract", "every OpenAPI operation x declared response status", backendContract},
	{"backend-e2e", "backend", "e2e", "reviewed API journeys (Playwright)", inventoryGate("backend-e2e.yaml", "BE-E2E-", paths.E2EResults, playwrightJSON)},
	{"frontend-unit", "frontend", "unit", "statements + branches (@vitest/coverage-v8)", frontendUnit},
	{"frontend-integration", "frontend", "integration", "reviewed component behaviors + every UI surface inventoried", frontendIntegration},
	{"frontend-contract", "frontend", "contract", "consumed OpenAPI operations x declared response status", frontendContract},
	{"frontend-e2e", "frontend", "e2e", "reviewed UI journeys (Playwright)", inventoryGate("frontend-e2e.yaml", "FE-E2E-", paths.E2EResults, playwrightJSON)},
	{"backend-consolidated", "backend", "consolidated", "every statement, executed by any layer (unit + integration + contract + e2e merged)", backendConsolidated},
	{"frontend-consolidated", "frontend", "consolidated", "every line, executed by any layer (unit + integration + e2e merged)", frontendConsolidatedGate},
}

func fail(r *GateResult, format string, args ...any) []Element {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
	return nil
}

func backendUnit(root string, r *GateResult) []Element {
	blocks, err := parseGoProfile(filepath.Join(root, paths.BackendUnitProfile), backendModule)
	if err != nil {
		return fail(r, "missing evidence: %v", err)
	}
	r.Errors = append(r.Errors, missingPackages(filepath.Join(root, "backend"), blocks)...)
	n, err := decisionPoints(filepath.Join(root, "backend"), func(rel string) bool {
		return strings.HasPrefix(rel, "internal/") && !strings.Contains(rel, "db/") && !strings.Contains(rel, "/postgres/")
	})
	if err == nil {
		r.Notes = append(r.Notes, fmt.Sprintf(
			"branch inventory (informative, not gated): %d decision points in the unit scope; Go's native coverage measures statements only, so branch outcomes are covered by table-driven cases reviewed per decision point", n))
	}
	return goStatementElements(blocks)
}

func backendIntegration(root string, r *GateResult) []Element {
	blocks, err := parseGoProfile(filepath.Join(root, paths.BackendIntProfile), backendModule)
	if err != nil {
		return fail(r, "missing evidence: %v", err)
	}
	ids, err := goTestJSON(filepath.Join(root, paths.BackendIntJSON))
	if err != nil {
		return fail(r, "missing evidence: %v", err)
	}
	var out []Element
	// Auto-derived surfaces: every sqlc query of every module must run against Postgres.
	queryFiles, _ := filepath.Glob(filepath.Join(root, "backend/queries/*.sql"))
	nameRe := regexp.MustCompile(`(?m)^-- name: (\w+)`)
	for _, qf := range queryFiles {
		module := strings.TrimSuffix(filepath.Base(qf), ".sql")
		raw, _ := os.ReadFile(qf)
		genDir := filepath.Join(root, "backend/internal", module, module+"db")
		methods := map[string][2]int{}
		methodFile := map[string]string{}
		gen, _ := filepath.Glob(filepath.Join(genDir, "*.go"))
		for _, g := range gen {
			ms, err := queryMethods(g)
			if err != nil {
				return fail(r, "parse %s: %v", g, err)
			}
			rel, _ := filepath.Rel(filepath.Join(root, "backend"), g)
			for k, v := range ms {
				methods[k] = v
				methodFile[k] = filepath.ToSlash(rel)
			}
		}
		for _, m := range nameRe.FindAllStringSubmatch(string(raw), -1) {
			id := fmt.Sprintf("BE-INT-Q:%s.%s", module, m[1])
			span, ok := methods[m[1]]
			covered := ok && funcCovered(blocks, methodFile[m[1]], span[0], span[1])
			out = append(out, Element{Metric: "targets", Key: id, Label: id + " (sqlc query never executed against Postgres)", Covered: covered})
		}
	}
	inv, err := loadInventory(filepath.Join(root, paths.Inventories, "backend-integration.yaml"))
	if err != nil {
		return fail(r, "%v", err)
	}
	for _, t := range inv {
		out = append(out, Element{Metric: "targets", Key: t.ID, Label: t.ID + " " + t.Description, Covered: ids.covered(t.ID)})
	}
	undeclared(r, ids, "BE-INT-", inv)
	return out
}

// undeclared reports test ids the inventory does not declare as gate errors.
func undeclared(r *GateResult, ids testIDs, prefix string, inv []InventoryTarget) {
	for _, id := range ids.unknown(prefix, inv) {
		r.Errors = append(r.Errors, "test id "+id+" is not in the inventory (add it, or fix the test name)")
	}
}

func contractElements(root string, r *GateResult, evidencePath string, include func(Variant) bool) []Element {
	variants, err := loadVariants(filepath.Join(root, paths.Spec))
	if err != nil {
		return fail(r, "contract: %v", err)
	}
	validated, err := contractEvidence(filepath.Join(root, evidencePath))
	if err != nil {
		return fail(r, "missing evidence: %v", err)
	}
	var out []Element
	for _, v := range variants {
		if include(v) {
			out = append(out, Element{Metric: "variants", Key: v.ID(), Label: v.Key + " -> " + v.Status, Covered: validated[v.ID()]})
		}
	}
	return out
}

func backendContract(root string, r *GateResult) []Element {
	return contractElements(root, r, paths.BackendContract, func(Variant) bool { return true })
}

func frontendContract(root string, r *GateResult) []Element {
	consumed, err := consumedOperations(filepath.Join(root, paths.FrontendQueries))
	if err != nil {
		return fail(r, "%v", err)
	}
	r.Notes = append(r.Notes, fmt.Sprintf("%d operations consumed by the frontend (derived from generated-client calls)", len(consumed)))
	return contractElements(root, r, paths.FrontendContract, func(v Variant) bool { return consumed[v.Key] })
}

func inventoryGate(file, prefix, results string, read func(string) (testIDs, error)) gateFunc {
	return func(root string, r *GateResult) []Element {
		inv, err := loadInventory(filepath.Join(root, paths.Inventories, file))
		if err != nil {
			return fail(r, "%v", err)
		}
		ids, err := read(filepath.Join(root, results))
		if err != nil {
			return fail(r, "missing evidence: %v", err)
		}
		var out []Element
		for _, t := range inv {
			out = append(out, Element{Metric: "journeys", Key: t.ID, Label: t.ID + " " + t.Description, Covered: ids.covered(t.ID)})
		}
		undeclared(r, ids, prefix, inv)
		return out
	}
}

func frontendUnit(root string, r *GateResult) []Element {
	m, err := readIstanbul(filepath.Join(root, paths.FrontendUnit))
	if err != nil {
		return fail(r, "missing evidence: %v", err)
	}
	return istanbulElements(m, "src/")
}

func frontendIntegration(root string, r *GateResult) []Element {
	inv, err := loadInventory(filepath.Join(root, paths.Inventories, "frontend-integration.yaml"))
	if err != nil {
		return fail(r, "%v", err)
	}
	ids, err := vitestJSON(filepath.Join(root, paths.FrontendIntJSON))
	if err != nil {
		return fail(r, "missing evidence: %v", err)
	}
	var out []Element
	referenced := map[string]bool{}
	for _, t := range inv {
		for _, s := range t.Surfaces {
			referenced[s] = true
		}
		out = append(out, Element{Metric: "targets", Key: t.ID, Label: t.ID + " " + t.Description, Covered: ids.covered(t.ID)})
	}
	// Auto-discovered surfaces: every UI component/page must be inventoried.
	var surfaces []string
	_ = filepath.WalkDir(filepath.Join(root, "frontend/src"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(root, "frontend"), p)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".tsx") && !strings.Contains(rel, ".test.") && !strings.HasPrefix(rel, "src/components/ui/") &&
			!strings.HasPrefix(rel, "src/test/") && rel != "src/main.tsx" {
			surfaces = append(surfaces, rel)
		}
		return nil
	})
	sort.Strings(surfaces)
	for _, s := range surfaces {
		out = append(out, Element{Metric: "surfaces", Key: "surface:" + s, Label: s + " is not referenced by any integration target", Covered: referenced[s]})
	}
	for s := range referenced {
		if _, err := os.Stat(filepath.Join(root, "frontend", s)); err != nil {
			r.Errors = append(r.Errors, "inventory references a missing surface: "+s)
		}
	}
	undeclared(r, ids, "FE-INT-", inv)
	return out
}
