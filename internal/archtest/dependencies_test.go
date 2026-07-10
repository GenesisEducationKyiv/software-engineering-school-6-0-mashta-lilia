// Package archtest verifies the dependency-inversion rule from ADR 0001 by
// inspecting the module's real import graph, not by trusting the diagram in
// docs/system-design.md §3.3.
package archtest

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github-release-notifier"

type layer int

const (
	layerPlatform layer = iota
	layerDomain
	layerAdapter
	layerRoot
	// layerTestInfra is test-only tooling (this package itself, and the
	// tests/ integration harness) — not part of the running application, so
	// it's outside the layering rule entirely rather than a violation of it.
	layerTestInfra
)

func (l layer) String() string {
	switch l {
	case layerPlatform:
		return "platform"
	case layerDomain:
		return "domain"
	case layerAdapter:
		return "adapter"
	case layerRoot:
		return "composition-root"
	case layerTestInfra:
		return "test-infra"
	default:
		return "unknown"
	}
}

// classify maps a package's import path, relative to the module root, to its
// architectural layer. Domain and composition-root entries are exact matches
// on purpose: a newly added package must be classified here explicitly
// instead of silently inheriting a sibling's layer.
func classify(rel string) (layer, bool) {
	switch {
	case rel == "internal/archtest", hasPrefix(rel, "tests"):
		return layerTestInfra, true
	case rel == "main", rel == "internal/app",
		rel == "services/notification/main", rel == "services/notification/app":
		return layerRoot, true
	case hasPrefix(rel, "internal/platform"), hasPrefix(rel, "internal/gen"),
		rel == "internal/messaging", rel == "internal/notifyevent", rel == "internal/sagaevent",
		rel == "internal/config", rel == "services/notification/config",
		rel == "services/notification/migrations":
		return layerPlatform, true
	case hasPrefix(rel, "internal/api"), hasPrefix(rel, "internal/client"),
		rel == "services/notification/consumer", rel == "services/notification/grpcserver",
		rel == "services/notification/resthttp", rel == "services/notification/sagaparticipant",
		rel == "services/notification/smtp":
		return layerAdapter, true
	case rel == "internal/subscription", rel == "internal/release", rel == "internal/saga",
		rel == "internal/email", rel == "internal/repository",
		rel == "services/notification", rel == "services/notification/store":
		return layerDomain, true
	default:
		return 0, false
	}
}

func hasPrefix(rel, pkg string) bool {
	return rel == pkg || strings.HasPrefix(rel, pkg+"/")
}

func relPath(importPath string) string {
	return strings.TrimPrefix(strings.TrimPrefix(importPath, modulePath), "/")
}

// TestDomainPackagesDoNotImportAdaptersOrCompositionRoot is the automated
// version of ADR 0001's rule: business logic never depends on how it's
// delivered (HTTP/gRPC/broker) or how the app is wired together. It classifies
// every package in the module and, for each one in the domain layer, checks
// every direct import against the classification of every other package —
// so a violation anywhere in the chain surfaces at the package that
// introduces it, not just at some distant caller.
//
// packages.Load reads the build graph from disk at run time rather than
// through a Go import, so Go's test cache can't see when an unrelated source
// file changes; use `go test -count=1` if you edit other files and rerun this
// test without touching this package.
func TestDomainPackagesDoNotImportAdaptersOrCompositionRoot(t *testing.T) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports}
	pkgs, err := packages.Load(cfg, modulePath+"/...")
	if err != nil {
		t.Fatalf("loading packages: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("packages.Load returned no packages — module path or pattern is wrong")
	}

	var unclassified []string
	var violations []string
	for _, pkg := range pkgs {
		for _, perr := range pkg.Errors {
			t.Errorf("package %s: %v", pkg.PkgPath, perr)
		}

		rel := relPath(pkg.PkgPath)
		l, ok := classify(rel)
		if !ok {
			unclassified = appendUnique(unclassified, rel)
			continue
		}
		if l != layerDomain {
			continue
		}

		for importPath := range pkg.Imports {
			if !strings.HasPrefix(importPath, modulePath) {
				continue // stdlib or third-party: not subject to this rule
			}
			importRel := relPath(importPath)
			importLayer, iok := classify(importRel)
			if !iok {
				unclassified = appendUnique(unclassified, importRel)
				continue
			}
			if importLayer == layerAdapter || importLayer == layerRoot {
				violations = append(violations, fmt.Sprintf(
					"%s (domain) imports %s (%s) — domain packages must not depend on adapters or the composition root",
					rel, importRel, importLayer,
				))
			}
		}
	}

	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Fatalf("unclassified package(s), add to archtest.classify: %s", strings.Join(unclassified, ", "))
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("architecture violations:\n%s", strings.Join(violations, "\n"))
	}
}

func appendUnique(in []string, s string) []string {
	for _, existing := range in {
		if existing == s {
			return in
		}
	}
	return append(in, s)
}
