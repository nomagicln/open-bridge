package spec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBenchmarkIndexSpec builds a ~4MB OpenAPI spec (8000 operations) in a temp
// directory and returns its path. The spec is generated deterministically so the
// benchmark can be reproduced without checking a large fixture into the repo.
func writeBenchmarkIndexSpec(b *testing.B) string {
	b.Helper()

	const resources = 4000

	description := strings.Repeat("Benchmark operation description for derived index. ", 5)

	var sb strings.Builder
	sb.WriteString(`{"openapi":"3.0.0","info":{"title":"Derived Index Benchmark API","version":"1.0.0"},"paths":{`)

	for i := range resources {
		if i > 0 {
			sb.WriteByte(',')
		}

		fmt.Fprintf(&sb, `"/resource%d":{`+
			`"get":{"operationId":"listResource%d","summary":"List resource %d","description":%q,`+
			`"tags":["tag%d"],"parameters":[{"name":"limit","in":"query","schema":{"type":"integer"}},`+
			`{"name":"cursor","in":"query","schema":{"type":"string"}}],`+
			`"responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object"}}}}}},`+
			`"post":{"operationId":"createResource%d","summary":"Create resource %d","description":%q,`+
			`"requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object",`+
			`"properties":{"name":{"type":"string"},"description":{"type":"string"}}}}}},`+
			`"responses":{"201":{"description":"Created"}}}}`,
			i, i, i, description, i%50, i, i, description)
	}

	sb.WriteString(`}}`)

	specPath := filepath.Join(b.TempDir(), "bench-index-spec.json")
	if err := os.WriteFile(specPath, []byte(sb.String()), 0o600); err != nil {
		b.Fatalf("write benchmark spec: %v", err)
	}

	b.Logf("benchmark spec size: %d bytes", sb.Len())

	return specPath
}

// BenchmarkLoadDerivedIndexCold measures a full miss: parse the 4MB spec, build
// the derived index and persist it.
func BenchmarkLoadDerivedIndexCold(b *testing.B) {
	specPath := writeBenchmarkIndexSpec(b)
	cacheDir := b.TempDir()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "bench"); err != nil {
			b.Fatalf("LoadDerivedIndex: %v", err)
		}
	}
}

// BenchmarkLoadDerivedIndexWarm measures a persisted index hit: fresh parser, one
// file read + CRC check + gob decode, no spec parsing.
func BenchmarkLoadDerivedIndexWarm(b *testing.B) {
	specPath := writeBenchmarkIndexSpec(b)
	cacheDir := b.TempDir()

	if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "bench"); err != nil {
		b.Fatalf("prime index: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "bench")
		if err != nil || idx == nil {
			b.Fatalf("warm LoadDerivedIndex: idx=%v err=%v", idx, err)
		}
	}
}

// BenchmarkDerivedIndexCommandTree measures converting a decoded index back into
// the semantic command tree used by help and completion.
func BenchmarkDerivedIndexCommandTree(b *testing.B) {
	specPath := writeBenchmarkIndexSpec(b)
	cacheDir := b.TempDir()

	idx, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "bench")
	if err != nil || idx == nil {
		b.Fatalf("load index: idx=%v err=%v", idx, err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tree := idx.CommandTree()
		if len(tree.RootResources) == 0 {
			b.Fatal("empty command tree")
		}
	}
}

// BenchmarkDerivedIndexEncode measures the gob payload encoding used by store.
func BenchmarkDerivedIndexEncode(b *testing.B) {
	specPath := writeBenchmarkIndexSpec(b)
	idx, err := NewParser(WithIndexCache(b.TempDir())).LoadDerivedIndex(context.Background(), specPath, "bench")
	if err != nil || idx == nil {
		b.Fatalf("load index: idx=%v err=%v", idx, err)
	}
	key := fileDerivedIndexKeyMustBench(b, specPath, "bench")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		payload, err := marshalDerivedIndexPayload(idx)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		if _, err := encodeDerivedIndexFile(key, payload); err != nil {
			b.Fatalf("encode: %v", err)
		}
	}
}

// fileDerivedIndexKeyMustBench builds a derived index key or fails the benchmark.
func fileDerivedIndexKeyMustBench(b *testing.B, source, appName string) derivedIndexKey {
	b.Helper()

	key, err := fileDerivedIndexKey(source, appName)
	if err != nil {
		b.Fatalf("fileDerivedIndexKey: %v", err)
	}

	return key
}
