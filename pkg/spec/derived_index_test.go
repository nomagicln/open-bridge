package spec

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nomagicln/open-bridge/internal/proptest"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

const derivedIndexSpecJSON = `{
  "openapi": "3.0.0",
  "info": {"title": "Derived Index API", "version": "2.0.0"},
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List pets",
        "description": "Lists every pet",
        "tags": ["pets"],
        "responses": {"200": {"description": "OK"}}
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create pet",
        "tags": ["pets"],
        "responses": {"201": {"description": "Created"}}
      }
    },
    "/toys": {
      "get": {
        "operationId": "listToys",
        "summary": "List toys",
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`

// writeDerivedIndexSpec writes the shared test spec and returns its path.
func writeDerivedIndexSpec(t *testing.T) string {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "index-spec.json")
	if err := os.WriteFile(specPath, []byte(derivedIndexSpecJSON), 0o600); err != nil {
		t.Fatalf("write derived index spec: %v", err)
	}

	return specPath
}

// readIndexDirFiles returns all files in the index cache directory.
func readIndexDirFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		t.Fatalf("ReadDir(%s) failed: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// corruptFilePreservingStat rewrites path with garbage of the same size and
// restores its modification time, so a local index key still matches even
// though parsing the file would now fail.
func corruptFilePreservingStat(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) failed: %v", path, err)
	}

	garbage := bytes.Repeat([]byte("x"), int(info.Size()))
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatalf("corrupt spec: %v", err)
	}

	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after corrupt: %v", err)
	}
	if after.Size() != info.Size() || after.ModTime().UnixNano() != info.ModTime().UnixNano() {
		t.Fatalf("corruption changed the source key: size %d->%d mtime %d->%d",
			info.Size(), after.Size(), info.ModTime().UnixNano(), after.ModTime().UnixNano())
	}
}

func TestWithIndexCacheDisabledByEmptyDir(t *testing.T) {
	if p := NewParser(WithIndexCache("")); p.indexCache != nil {
		t.Error("expected empty index cache dir to disable the index cache")
	}

	dir := t.TempDir()
	p := NewParser(WithIndexCache(dir))
	if p.indexCache == nil {
		t.Fatal("expected non-empty index cache dir to enable the index cache")
	}
	if p.indexCache.dir != dir {
		t.Errorf("index cache dir = %q, want %q", p.indexCache.dir, dir)
	}
}

func TestLoadDerivedIndexDisabledReturnsNil(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)

	idx, err := NewParser().LoadDerivedIndex(context.Background(), specPath, "app")
	if err != nil {
		t.Fatalf("LoadDerivedIndex failed: %v", err)
	}
	if idx != nil {
		t.Errorf("expected nil index when the index cache is disabled, got %+v", idx)
	}
}

func TestLoadDerivedIndexBuildsPersistsAndReuses(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()

	parser := NewParser(WithIndexCache(cacheDir))
	idx, err := parser.LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("LoadDerivedIndex failed: %v", err)
	}
	if idx == nil {
		t.Fatal("expected a built derived index")
	}
	if idx.AppName != "demo" || idx.Source != specPath || idx.SourceKind != "file" {
		t.Errorf("unexpected index metadata: %+v", idx)
	}
	if idx.IndexSchemaVersion != DerivedIndexSchemaVersion {
		t.Errorf("IndexSchemaVersion = %d, want %d", idx.IndexSchemaVersion, DerivedIndexSchemaVersion)
	}
	if idx.ParserVersion != parserVersion() {
		t.Errorf("ParserVersion = %q, want %q", idx.ParserVersion, parserVersion())
	}
	if len(idx.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d (%+v)", len(idx.Resources), idx.Resources)
	}

	files := readIndexDirFiles(t, cacheDir)
	if len(files) != 1 {
		t.Fatalf("expected 1 index file, got %v", files)
	}
	info, err := os.Stat(filepath.Join(cacheDir, files[0]))
	if err != nil {
		t.Fatalf("Stat index file failed: %v", err)
	}
	if info.Mode().Perm() != derivedIndexFilePerm {
		t.Errorf("index file perm = %o, want %o", info.Mode().Perm(), derivedIndexFilePerm)
	}

	// A fresh parser must serve the same index, and a source whose content is
	// no longer parseable (same size/mtime, so the key still matches) proves the
	// second load did not parse the spec.
	second := NewParser(WithIndexCache(cacheDir))
	cached, err := second.LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("cached LoadDerivedIndex failed: %v", err)
	}
	if !reflect.DeepEqual(idx, cached) {
		t.Errorf("cached index differs from built index")
	}

	corruptFilePreservingStat(t, specPath)

	third := NewParser(WithIndexCache(cacheDir))
	fromCache, err := third.LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("index cache hit should not parse the spec: %v", err)
	}
	if !reflect.DeepEqual(idx, fromCache) {
		t.Errorf("index loaded after corrupting the spec differs")
	}
}

func TestLoadDerivedIndexRebuildsWhenSourceChanges(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()

	if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo"); err != nil {
		t.Fatalf("initial LoadDerivedIndex failed: %v", err)
	}

	updated := `{
  "openapi": "3.0.0",
  "info": {"title": "Derived Index API", "version": "2.0.0"},
  "paths": {
    "/pets": {"get": {"operationId": "listPets", "summary": "List pets", "responses": {"200": {"description": "OK"}}}},
    "/zebras": {"get": {"operationId": "listZebras", "summary": "List zebras", "responses": {"200": {"description": "OK"}}}}
  }
}`
	if err := os.WriteFile(specPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("update spec: %v", err)
	}

	idx, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("rebuild LoadDerivedIndex failed: %v", err)
	}

	names := make([]string, 0, len(idx.Resources))
	for _, res := range idx.Resources {
		names = append(names, res.Name)
	}
	if !reflect.DeepEqual(names, []string{"pets", "zebras"}) {
		t.Errorf("rebuilt resources = %v, want [pets zebras]", names)
	}
	if files := readIndexDirFiles(t, cacheDir); len(files) != 1 {
		t.Errorf("expected the stale index to be replaced in place, got %v", files)
	}
}

func TestDerivedIndexVersionMismatchRebuilds(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()

	if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo"); err != nil {
		t.Fatalf("initial LoadDerivedIndex failed: %v", err)
	}

	files := readIndexDirFiles(t, cacheDir)
	if len(files) != 1 {
		t.Fatalf("expected 1 index file, got %v", files)
	}
	indexPath := filepath.Join(cacheDir, files[0])

	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index file: %v", err)
	}
	binary.BigEndian.PutUint32(data[derivedIndexMagicSize:derivedIndexMagicSize+4], derivedIndexFormatVersion+1)
	if err := os.WriteFile(indexPath, data, derivedIndexFilePerm); err != nil {
		t.Fatalf("tamper index version: %v", err)
	}

	idx, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("expected transparent rebuild after version mismatch, got: %v", err)
	}
	if len(idx.Resources) != 2 {
		t.Errorf("rebuilt index has %d resources, want 2", len(idx.Resources))
	}

	repaired, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read repaired index: %v", err)
	}
	if got := binary.BigEndian.Uint32(repaired[derivedIndexMagicSize : derivedIndexMagicSize+4]); got != derivedIndexFormatVersion {
		t.Errorf("repaired format version = %d, want %d", got, derivedIndexFormatVersion)
	}
}

func TestDerivedIndexCorruptPayloadRebuilds(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()

	if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo"); err != nil {
		t.Fatalf("initial LoadDerivedIndex failed: %v", err)
	}

	indexPath := filepath.Join(cacheDir, readIndexDirFiles(t, cacheDir)[0])
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index file: %v", err)
	}
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(indexPath, data, derivedIndexFilePerm); err != nil {
		t.Fatalf("corrupt index payload: %v", err)
	}

	idx, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("expected rebuild after corrupt payload, got: %v", err)
	}
	if idx == nil || len(idx.Resources) != 2 {
		t.Errorf("unexpected rebuilt index: %+v", idx)
	}

	// Truncated file must also be treated as a miss.
	if err := os.WriteFile(indexPath, data[:5], derivedIndexFilePerm); err != nil {
		t.Fatalf("truncate index: %v", err)
	}
	if idx, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo"); err != nil || idx == nil {
		t.Errorf("expected rebuild after truncated index, got idx=%v err=%v", idx, err)
	}
}

func TestDerivedIndexFileFormatHeader(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()

	if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo"); err != nil {
		t.Fatalf("initial LoadDerivedIndex failed: %v", err)
	}

	indexPath := filepath.Join(cacheDir, readIndexDirFiles(t, cacheDir)[0])
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index file: %v", err)
	}

	if !bytes.Equal(data[:derivedIndexMagicSize], derivedIndexMagic[:]) {
		t.Errorf("index magic = %q, want %q", data[:derivedIndexMagicSize], derivedIndexMagic[:])
	}

	offset := derivedIndexMagicSize
	formatVersion := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	keyLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	payloadLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	checksum := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	if formatVersion != derivedIndexFormatVersion {
		t.Errorf("format version = %d, want %d", formatVersion, derivedIndexFormatVersion)
	}
	if int(offset)+int(keyLen)+int(payloadLen) != len(data) {
		t.Fatalf("header lengths do not add up: off=%d key=%d payload=%d file=%d",
			offset, keyLen, payloadLen, len(data))
	}
	payload := data[offset+int(keyLen):]
	if got := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli)); got != checksum {
		t.Errorf("payload CRC32C = %#x, want %#x", got, checksum)
	}

	var key derivedIndexKey
	if err := json.Unmarshal(data[offset:offset+int(keyLen)], &key); err != nil {
		t.Fatalf("decode key JSON: %v", err)
	}
	if key.AppName != "demo" || key.IndexSchemaVersion != DerivedIndexSchemaVersion || key.SourceKind != "file" {
		t.Errorf("unexpected persisted key: %+v", key)
	}
}

func TestDerivedIndexKeyIsolationPerApp(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()
	parser := NewParser(WithIndexCache(cacheDir))

	first, err := parser.LoadDerivedIndex(context.Background(), specPath, "appA")
	if err != nil {
		t.Fatalf("LoadDerivedIndex(appA) failed: %v", err)
	}
	second, err := parser.LoadDerivedIndex(context.Background(), specPath, "appB")
	if err != nil {
		t.Fatalf("LoadDerivedIndex(appB) failed: %v", err)
	}
	if first.AppName != "appA" || second.AppName != "appB" {
		t.Errorf("indexes not isolated per app: %q / %q", first.AppName, second.AppName)
	}
	if files := readIndexDirFiles(t, cacheDir); len(files) != 2 {
		t.Errorf("expected one index file per app, got %v", files)
	}
}

func TestLoadDerivedIndexMissingSource(t *testing.T) {
	parser := NewParser(WithIndexCache(t.TempDir()))
	missing := filepath.Join(t.TempDir(), "missing.json")

	if _, err := parser.LoadDerivedIndex(context.Background(), missing, "demo"); err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}

func TestDerivedIndexCommandTreeRoundTrip(t *testing.T) {
	tree := &semantic.CommandTree{RootResources: map[string]*semantic.Resource{
		"storeorder": {
			Name:        "storeorder",
			Description: "orders",
			Operations: map[string]*semantic.Operation{
				"list": {
					Name:        "list",
					Method:      "GET",
					Path:        "/store/order",
					OperationID: "listOrders",
					Summary:     "List orders",
					Description: "Lists orders",
					Resource:    "storeorder",
					Aliases:     []string{"all"},
				},
			},
			SubResources: map[string]*semantic.Resource{
				"orderitem": {
					Name: "orderitem",
					Operations: map[string]*semantic.Operation{
						"get": {Name: "get", Method: "GET", Path: "/store/order/item"},
					},
				},
			},
		},
	}}

	idx := newDerivedIndex("nested", "file", "/spec.json", tree)
	roundTripped := idx.CommandTree()

	root, ok := roundTripped.RootResources["storeorder"]
	if !ok {
		t.Fatalf("missing root resource, got %v", roundTripped.RootResources)
	}
	if root.Description != "orders" {
		t.Errorf("resource description = %q", root.Description)
	}
	op, ok := root.Operations["list"]
	if !ok {
		t.Fatalf("missing operation, got %v", root.Operations)
	}
	if op.OperationID != "listOrders" || op.Summary != "List orders" || op.Path != "/store/order" || op.Method != "GET" {
		t.Errorf("operation round-trip mismatch: %+v", op)
	}
	if !reflect.DeepEqual(op.Aliases, []string{"all"}) {
		t.Errorf("aliases = %v, want [all]", op.Aliases)
	}
	sub, ok := root.SubResources["orderitem"]
	if !ok {
		t.Fatalf("missing sub-resource, got %v", root.SubResources)
	}
	if _, ok := sub.Operations["get"]; !ok {
		t.Errorf("missing sub-resource operation: %v", sub.Operations)
	}
	if sub.Parent != root {
		t.Error("sub-resource parent pointer not restored")
	}
}

func TestDerivedIndexFromSpecIncludesOperationSummaries(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	parser := NewParser(WithIndexCache(t.TempDir()))

	idx, err := parser.LoadDerivedIndex(context.Background(), specPath, "demo")
	if err != nil {
		t.Fatalf("LoadDerivedIndex failed: %v", err)
	}

	tree := idx.CommandTree()
	pets, ok := tree.RootResources["pets"]
	if !ok {
		t.Fatalf("missing pets resource, got %v", tree.RootResources)
	}
	listOp, ok := pets.Operations["list"]
	if !ok {
		t.Fatalf("missing list verb, got %v", pets.Operations)
	}
	if listOp.Method != "GET" || listOp.Path != "/pets" || listOp.Summary != "List pets" ||
		listOp.Description != "Lists every pet" || listOp.OperationID != "listPets" {
		t.Errorf("unexpected list operation: %+v", listOp)
	}
	if !reflect.DeepEqual(listOp.Tags, []string{"pets"}) {
		t.Errorf("tags = %v, want [pets]", listOp.Tags)
	}
	if _, ok := pets.Operations["create"]; !ok {
		t.Errorf("missing create verb, got %v", pets.Operations)
	}
}

func TestDerivedIndexConcurrentLoad(t *testing.T) {
	specPath := writeDerivedIndexSpec(t)
	cacheDir := t.TempDir()

	if _, err := NewParser(WithIndexCache(cacheDir)).LoadDerivedIndex(context.Background(), specPath, "demo"); err != nil {
		t.Fatalf("initial LoadDerivedIndex failed: %v", err)
	}

	parser := NewParser(WithIndexCache(cacheDir))
	var wg sync.WaitGroup
	var failures atomic.Int32
	for range 16 {
		wg.Go(func() {
			idx, err := parser.LoadDerivedIndex(context.Background(), specPath, "demo")
			if err != nil || idx == nil || len(idx.Resources) != 2 {
				failures.Add(1)
			}
		})
	}
	wg.Wait()

	if failures.Load() != 0 {
		t.Errorf("%d concurrent index loads failed", failures.Load())
	}
}

func TestLoadDerivedIndexRemoteReusesIndex(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte(derivedIndexSpecJSON))
	}))
	defer server.Close()

	remoteDir := t.TempDir()
	indexDir := t.TempDir()
	source := server.URL + "/openapi.json"

	first := NewParser(WithRemoteCache(remoteDir), WithIndexCache(indexDir))
	idx, err := first.LoadDerivedIndex(context.Background(), source, "remoteapp")
	if err != nil {
		t.Fatalf("remote LoadDerivedIndex failed: %v", err)
	}
	if idx.ContentSHA256 == "" {
		t.Error("expected remote index to carry the content SHA256")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 network request, got %d", got)
	}

	second := NewParser(WithRemoteCache(remoteDir), WithIndexCache(indexDir))
	cached, err := second.LoadDerivedIndex(context.Background(), source, "remoteapp")
	if err != nil {
		t.Fatalf("cached remote LoadDerivedIndex failed: %v", err)
	}
	if !reflect.DeepEqual(idx, cached) {
		t.Error("cached remote index differs from built index")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("expected index cache hit to avoid network, got %d requests", got)
	}
}

func TestLoadDerivedIndexRemoteWithoutContentCacheReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(derivedIndexSpecJSON))
	}))
	defer server.Close()

	parser := NewParser(WithIndexCache(t.TempDir()))
	idx, err := parser.LoadDerivedIndex(context.Background(), server.URL+"/openapi.json", "remoteapp")
	if err != nil {
		t.Fatalf("LoadDerivedIndex failed: %v", err)
	}
	if idx != nil {
		t.Errorf("expected nil index without a content cache (cannot key safely), got %+v", idx)
	}
}

func TestParserVersionFingerprint(t *testing.T) {
	version := parserVersion()
	for _, component := range []string{"kin-openapi/", "semantic/", "schema/"} {
		if !bytes.Contains([]byte(version), []byte(component)) {
			t.Errorf("parser version %q missing component %q", version, component)
		}
	}
	if got := parserVersion(); got != version {
		t.Errorf("parserVersion is not stable: %q != %q", got, version)
	}
}

// TestPropertyDerivedIndexGobRoundTrip validates that the persisted DTO survives
// gob encoding without losing fields (the failure mode that ruled out encoding
// openapi3.T directly).
func TestPropertyDerivedIndexGobRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	properties := gopter.NewProperties(proptest.TestParameters())
	properties.Property("derived index gob round-trip preserves data", prop.ForAll(
		func(appName, verb, method, path, operationID, summary string, size int64) bool {
			original := &DerivedIndex{
				IndexSchemaVersion: DerivedIndexSchemaVersion,
				ParserVersion:      parserVersion(),
				AppName:            appName,
				SourceKind:         "file",
				Source:             "/tmp/spec.json",
				Size:               size,
				Resources: []DerivedResource{
					{
						Name:        "pets",
						Description: "pets",
						Operations: []DerivedOperation{
							{
								Verb:        verb,
								Method:      method,
								Path:        path,
								OperationID: operationID,
								Summary:     summary,
								Tags:        []string{"pets"},
							},
						},
					},
				},
			}

			payload, err := marshalDerivedIndexPayload(original)
			if err != nil {
				return false
			}

			decoded, err := unmarshalDerivedIndexPayload(payload)
			if err != nil {
				return false
			}

			return reflect.DeepEqual(original, decoded)
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.Int64(),
	))

	properties.TestingRun(t)
}
