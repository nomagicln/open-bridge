// Package spec provides OpenAPI specification parsing and validation.
// This file implements the persistent derived index: a compact, versioned
// representation of the semantic command tree that application help and shell
// completion can render without re-parsing the whole OpenAPI document.
//
// The index uses explicit exported DTOs (never gob-encoded third-party
// structures): kin-openapi's openapi3.T contains unexported fields (for example
// openapi3.Paths) that gob silently drops, which corrupts the payload without
// returning an error.
package spec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/nomagicln/open-bridge/pkg/semantic"
)

const (
	// DerivedIndexSchemaVersion is bumped whenever the DerivedIndex DTO fields or
	// their semantics change in a way that invalidates persisted indexes.
	DerivedIndexSchemaVersion = 1
	// derivedIndexFormatVersion is the on-disk container format version.
	derivedIndexFormatVersion = 1
	// derivedIndexMagicSize is the length of the file magic.
	derivedIndexMagicSize = 8
	// derivedIndexDirPerm is the permission used for the index cache directory.
	derivedIndexDirPerm = 0o755
	// derivedIndexFilePerm is the permission used for index files.
	derivedIndexFilePerm = 0o600
	// fallbackKinOpenAPIVersion is used when build information about the
	// kin-openapi module is unavailable.
	fallbackKinOpenAPIVersion = "v0.149.0"
	// indexFileNamePrefix is the prefix of index files in the cache directory.
	indexFileNamePrefix = "index-"
	// indexFileNameHashLength is the number of hash characters kept in the name.
	indexFileNameHashLength = 16
)

// derivedIndexMagic identifies a derived index file and its container version.
var derivedIndexMagic = [derivedIndexMagicSize]byte{'O', 'B', 'D', 'I', 'D', 'X', '0', '1'}

// castagnoliTable is the CRC32C table used for payload integrity checks.
var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

// parserVersionOnce caches the computed parser version fingerprint.
var parserVersionOnce sync.Once

// parserVersionValue caches the computed parser version fingerprint.
var parserVersionValue string

// DerivedIndex is the persisted, versioned representation of a semantic command
// tree plus the operation summaries needed to render application help.
type DerivedIndex struct {
	// IndexSchemaVersion is the DTO schema version that produced this index.
	IndexSchemaVersion int
	// ParserVersion fingerprints the parser and semantic derivation rules.
	ParserVersion string
	// AppName isolates indexes of the same source installed under different apps.
	AppName string
	// SourceKind is "file" or "url".
	SourceKind string
	// Source is the absolute file path or the normalized URL.
	Source string
	// Size is the source file size in bytes (local sources only).
	Size int64
	// ModTimeNanos is the source modification time (local sources only).
	ModTimeNanos int64
	// ContentSHA256 is the remote content digest (remote sources only).
	ContentSHA256 string
	// Resources contains the command tree, sorted by resource name.
	Resources []DerivedResource
}

// DerivedResource is a persisted command-tree resource.
type DerivedResource struct {
	Name         string
	Description  string
	Operations   []DerivedOperation
	SubResources []DerivedResource
}

// DerivedOperation is the operation summary needed by application help.
type DerivedOperation struct {
	Verb        string
	Method      string
	Path        string
	OperationID string
	Summary     string
	Description string
	Resource    string
	Aliases     []string
	Tags        []string
}

// derivedIndexKey identifies one persisted index entry and is embedded in the
// file header (and hashed into the file name).
type derivedIndexKey struct {
	AppName            string `json:"app"`
	SourceKind         string `json:"kind"`
	Source             string `json:"source"`
	Size               int64  `json:"size,omitempty"`
	ModTimeNanos       int64  `json:"mtime,omitempty"`
	ContentSHA256      string `json:"sha256,omitempty"`
	ParserVersion      string `json:"parser_version"`
	IndexSchemaVersion int    `json:"schema_version"`
}

// derivedIndexCache persists derived indexes in a directory.
type derivedIndexCache struct {
	dir string
}

// WithIndexCache enables the persistent derived-index cache. An empty dir
// disables the index cache; callers then fall back to parsing the spec.
func WithIndexCache(dir string) ParserOption {
	return func(p *Parser) {
		if dir == "" {
			p.indexCache = nil
			return
		}

		p.indexCache = &derivedIndexCache{dir: dir}
	}
}

// LoadDerivedIndex returns the derived command-tree index for source.
//
// When the persistent index cache is disabled it returns (nil, nil) so callers
// fall back to the regular spec parsing path. When enabled, a matching index is
// read from disk; otherwise the spec is parsed once, the index is built, and it
// is persisted atomically for subsequent processes.
func (p *Parser) LoadDerivedIndex(ctx context.Context, source, appName string) (*DerivedIndex, error) {
	if p.indexCache == nil {
		return nil, nil
	}

	if isURL(source) {
		return p.loadRemoteDerivedIndex(ctx, source, appName)
	}

	return p.loadFileDerivedIndex(ctx, source, appName)
}

// loadFileDerivedIndex serves a local file source from the index cache, building
// and persisting the index on a miss. The key is based on the file stat, so no
// content hash is needed.
func (p *Parser) loadFileDerivedIndex(ctx context.Context, source, appName string) (*DerivedIndex, error) {
	key, err := fileDerivedIndexKey(source, appName)
	if err != nil {
		return nil, err
	}

	if idx := p.indexCache.load(key); idx != nil {
		return idx, nil
	}

	doc, err := p.LoadSpecWithContext(ctx, source)
	if err != nil {
		return nil, err
	}

	idx := newDerivedIndex(appName, "file", key.Source, semantic.NewMapper().BuildCommandTree(doc))
	idx.Size = key.Size
	idx.ModTimeNanos = key.ModTimeNanos

	_ = p.indexCache.store(key, idx)

	return idx, nil
}

// loadRemoteDerivedIndex serves a remote source from the index cache. The index
// key uses the content SHA256 from the remote content cache, so a fresh remote
// cache entry lets the index be loaded without any network access. Without a
// content cache the content hash is unavailable (and a URL-only key could serve
// stale data), so no index is used.
func (p *Parser) loadRemoteDerivedIndex(ctx context.Context, source, appName string) (*DerivedIndex, error) {
	if p.remoteCache == nil {
		return nil, nil
	}

	now := time.Now()
	if data, meta := p.remoteCache.load(source); data != nil && meta != nil && !meta.isExpired(now) {
		if idx := p.indexCache.load(urlDerivedIndexKey(source, appName, meta.SHA256)); idx != nil {
			return idx, nil
		}
	}

	doc, err := p.LoadSpecWithContext(ctx, source)
	if err != nil {
		return nil, err
	}

	idx := newDerivedIndex(appName, "url", source, semantic.NewMapper().BuildCommandTree(doc))

	if _, meta := p.remoteCache.load(source); meta != nil && meta.SHA256 != "" {
		key := urlDerivedIndexKey(source, appName, meta.SHA256)
		idx.ContentSHA256 = meta.SHA256
		_ = p.indexCache.store(key, idx)
	}

	return idx, nil
}

// CommandTree converts the persisted index back into a semantic command tree
// usable by the existing help and completion renderers.
func (d *DerivedIndex) CommandTree() *semantic.CommandTree {
	tree := &semantic.CommandTree{RootResources: make(map[string]*semantic.Resource, len(d.Resources))}

	for _, resource := range d.Resources {
		tree.RootResources[resource.Name] = resource.commandTreeResource()
	}

	return tree
}

// commandTreeResource converts one persisted resource (and its nested
// sub-resources) into a semantic resource.
func (r DerivedResource) commandTreeResource() *semantic.Resource {
	resource := &semantic.Resource{
		Name:         r.Name,
		Description:  r.Description,
		Operations:   make(map[string]*semantic.Operation, len(r.Operations)),
		SubResources: make(map[string]*semantic.Resource, len(r.SubResources)),
	}

	for _, op := range r.Operations {
		operation := &semantic.Operation{
			Name:        op.Verb,
			Method:      op.Method,
			Path:        op.Path,
			OperationID: op.OperationID,
			Summary:     op.Summary,
			Description: op.Description,
			Resource:    op.Resource,
			Aliases:     append([]string(nil), op.Aliases...),
			Tags:        append([]string(nil), op.Tags...),
		}
		resource.Operations[op.Verb] = operation
	}

	for _, sub := range r.SubResources {
		child := sub.commandTreeResource()
		child.Parent = resource
		resource.SubResources[child.Name] = child
	}

	return resource
}

// newDerivedIndex converts a semantic command tree into the persisted DTO.
func newDerivedIndex(appName, sourceKind, source string, tree *semantic.CommandTree) *DerivedIndex {
	idx := &DerivedIndex{
		IndexSchemaVersion: DerivedIndexSchemaVersion,
		ParserVersion:      parserVersion(),
		AppName:            appName,
		SourceKind:         sourceKind,
		Source:             source,
	}

	if tree == nil {
		return idx
	}

	names := sortedResourceNames(tree.RootResources)

	for _, name := range names {
		idx.Resources = append(idx.Resources, newDerivedResource(name, tree.RootResources[name]))
	}

	return idx
}

// newDerivedResource converts one semantic resource (and its nested
// sub-resources) into the persisted DTO.
func newDerivedResource(name string, resource *semantic.Resource) DerivedResource {
	out := DerivedResource{Name: name, Description: resource.Description}

	verbs := sortedOperationNames(resource.Operations)

	for _, verb := range verbs {
		op := resource.Operations[verb]
		if op == nil {
			continue
		}

		out.Operations = append(out.Operations, DerivedOperation{
			Verb:        verb,
			Method:      op.Method,
			Path:        op.Path,
			OperationID: op.OperationID,
			Summary:     op.Summary,
			Description: op.Description,
			Resource:    op.Resource,
			Aliases:     append([]string(nil), op.Aliases...),
			Tags:        append([]string(nil), op.Tags...),
		})
	}

	subNames := sortedResourceNames(resource.SubResources)

	for _, subName := range subNames {
		out.SubResources = append(out.SubResources, newDerivedResource(subName, resource.SubResources[subName]))
	}

	return out
}

// sortedResourceNames returns the sorted keys of a resource map.
func sortedResourceNames(resources map[string]*semantic.Resource) []string {
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// sortedOperationNames returns the sorted keys of an operation map.
func sortedOperationNames(operations map[string]*semantic.Operation) []string {
	names := make([]string, 0, len(operations))
	for name := range operations {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// fileDerivedIndexKey builds the cache key for a local file source.
func fileDerivedIndexKey(source, appName string) (derivedIndexKey, error) {
	absPath, err := filepath.Abs(source)
	if err != nil {
		return derivedIndexKey{}, fmt.Errorf("failed to resolve spec source '%s': %w", source, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return derivedIndexKey{}, fmt.Errorf("failed to stat spec source '%s': %w", source, err)
	}

	return derivedIndexKey{
		AppName:            appName,
		SourceKind:         "file",
		Source:             absPath,
		Size:               info.Size(),
		ModTimeNanos:       info.ModTime().UnixNano(),
		ParserVersion:      parserVersion(),
		IndexSchemaVersion: DerivedIndexSchemaVersion,
	}, nil
}

// urlDerivedIndexKey builds the cache key for a remote source.
func urlDerivedIndexKey(source, appName, contentSHA256 string) derivedIndexKey {
	return derivedIndexKey{
		AppName:            appName,
		SourceKind:         "url",
		Source:             source,
		ContentSHA256:      contentSHA256,
		ParserVersion:      parserVersion(),
		IndexSchemaVersion: DerivedIndexSchemaVersion,
	}
}

// parserVersion returns the version fingerprint embedded in index keys. It
// combines the kin-openapi version, the semantic derivation rules version and
// the index DTO schema version, so any of those changing invalidates the cache.
func parserVersion() string {
	parserVersionOnce.Do(func() {
		parserVersionValue = fmt.Sprintf("kin-openapi/%s;semantic/%d;schema/%d",
			kinOpenAPIModuleVersion(), semantic.DerivedRulesVersion, DerivedIndexSchemaVersion)
	})

	return parserVersionValue
}

// kinOpenAPIModuleVersion returns the kin-openapi version used to build this
// binary, when available from build information.
func kinOpenAPIModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallbackKinOpenAPIVersion
	}

	for _, dep := range info.Deps {
		if dep.Path != "github.com/getkin/kin-openapi" {
			continue
		}

		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}

		if dep.Version != "" {
			return dep.Version
		}
	}

	return fallbackKinOpenAPIVersion
}

// path returns the index file path for key.
//
// The file name is derived from the stable identity (app, source kind, source)
// rather than from the full key, so a changed source (new size/mtime/content
// hash) overwrites its stale entry instead of leaving orphan files behind. The
// full key is stored in the file header and validated on every read.
func (c *derivedIndexCache) path(key derivedIndexKey) string {
	identity := key.AppName + "\x00" + key.SourceKind + "\x00" + key.Source
	sum := sha256.Sum256([]byte(identity))
	keyHash := hex.EncodeToString(sum[:])[:indexFileNameHashLength]

	return filepath.Join(c.dir, indexFileNamePrefix+keyHash+".gob")
}

// load reads and validates the persisted index for key. It returns nil when the
// entry is absent, stale, corrupt or unreadable (corrupt files are removed so
// the next store can rewrite them).
func (c *derivedIndexCache) load(key derivedIndexKey) *DerivedIndex {
	path := c.path(key)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	payload, err := decodeDerivedIndexFile(data, key)
	if err != nil {
		_ = os.Remove(path)

		return nil
	}

	idx, err := unmarshalDerivedIndexPayload(payload)
	if err != nil {
		_ = os.Remove(path)

		return nil
	}

	if !indexMatchesKey(idx, key) {
		_ = os.Remove(path)

		return nil
	}

	return idx
}

// indexMatchesKey verifies that a decoded index belongs to key.
func indexMatchesKey(idx *DerivedIndex, key derivedIndexKey) bool {
	return idx != nil &&
		idx.IndexSchemaVersion == key.IndexSchemaVersion &&
		idx.ParserVersion == key.ParserVersion &&
		idx.AppName == key.AppName &&
		idx.SourceKind == key.SourceKind &&
		idx.Source == key.Source
}

// store persists idx for key atomically: a temporary file in the cache
// directory is written, fsynced, chmodded to 0600 and renamed into place.
func (c *derivedIndexCache) store(key derivedIndexKey, idx *DerivedIndex) error {
	payload, err := marshalDerivedIndexPayload(idx)
	if err != nil {
		return err
	}

	data, err := encodeDerivedIndexFile(key, payload)
	if err != nil {
		return err
	}

	return writeIndexFileAtomically(c.dir, c.path(key), data)
}

// writeIndexFileAtomically persists data to path via a temporary file in dir:
// write, fsync, chmod 0600, then rename into place.
func writeIndexFileAtomically(dir, path string, data []byte) error {
	if err := os.MkdirAll(dir, derivedIndexDirPerm); err != nil {
		return fmt.Errorf("failed to create derived index directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".index-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary derived index file: %w", err)
	}

	tmpPath := tmpFile.Name()
	removeTmp := func() { _ = os.Remove(tmpPath) }

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		removeTmp()

		return fmt.Errorf("failed to write derived index: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		removeTmp()

		return fmt.Errorf("failed to sync derived index: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		removeTmp()

		return fmt.Errorf("failed to close derived index: %w", err)
	}

	if err := os.Chmod(tmpPath, derivedIndexFilePerm); err != nil {
		removeTmp()

		return fmt.Errorf("failed to set derived index permissions: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		removeTmp()

		return fmt.Errorf("failed to move derived index into place: %w", err)
	}

	return nil
}

// marshalDerivedIndexPayload gob-encodes the DTO payload.
func marshalDerivedIndexPayload(idx *DerivedIndex) ([]byte, error) {
	var buf bytes.Buffer

	if err := gob.NewEncoder(&buf).Encode(idx); err != nil {
		return nil, fmt.Errorf("failed to encode derived index: %w", err)
	}

	return buf.Bytes(), nil
}

// unmarshalDerivedIndexPayload gob-decodes the DTO payload.
func unmarshalDerivedIndexPayload(data []byte) (*DerivedIndex, error) {
	idx := &DerivedIndex{}

	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(idx); err != nil {
		return nil, fmt.Errorf("failed to decode derived index: %w", err)
	}

	return idx, nil
}

// encodeDerivedIndexFile wraps payload into the on-disk container format:
// magic | formatVersion | keyLen | payloadLen | crc32c(payload) | keyJSON | payload.
func encodeDerivedIndexFile(key derivedIndexKey, payload []byte) ([]byte, error) {
	keyJSON, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal derived index key: %w", err)
	}

	headerSize := derivedIndexMagicSize + 4*4
	data := make([]byte, headerSize+len(keyJSON)+len(payload))

	copy(data, derivedIndexMagic[:])

	offset := derivedIndexMagicSize
	binary.BigEndian.PutUint32(data[offset:], derivedIndexFormatVersion)
	offset += 4
	binary.BigEndian.PutUint32(data[offset:], uint32(len(keyJSON))) //nolint:gosec // key length is bounded by the struct size
	offset += 4
	binary.BigEndian.PutUint32(data[offset:], uint32(len(payload))) //nolint:gosec // payload length is bounded by the spec size
	offset += 4
	binary.BigEndian.PutUint32(data[offset:], crc32.Checksum(payload, castagnoliTable))
	offset += 4

	offset += copy(data[offset:], keyJSON)
	copy(data[offset:], payload)

	return data, nil
}

// decodeDerivedIndexFile validates the container header and returns the payload.
func decodeDerivedIndexFile(data []byte, expected derivedIndexKey) ([]byte, error) {
	headerSize := derivedIndexMagicSize + 4*4
	if len(data) < headerSize {
		return nil, fmt.Errorf("derived index file too short: %d bytes", len(data))
	}

	if !bytes.Equal(data[:derivedIndexMagicSize], derivedIndexMagic[:]) {
		return nil, fmt.Errorf("invalid derived index magic")
	}

	offset := derivedIndexMagicSize
	formatVersion := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	keyLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	payloadLen := binary.BigEndian.Uint32(data[offset:])
	offset += 4
	expectedCRC := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	if formatVersion != derivedIndexFormatVersion {
		return nil, fmt.Errorf("unsupported derived index format version %d", formatVersion)
	}

	if uint64(offset)+uint64(keyLen)+uint64(payloadLen) != uint64(len(data)) {
		return nil, fmt.Errorf("derived index file has inconsistent lengths")
	}

	var actualKey derivedIndexKey
	if err := json.Unmarshal(data[offset:offset+int(keyLen)], &actualKey); err != nil {
		return nil, fmt.Errorf("failed to decode derived index key: %w", err)
	}

	if actualKey != expected {
		return nil, fmt.Errorf("derived index key mismatch")
	}

	payload := data[offset+int(keyLen):]
	if crc32.Checksum(payload, castagnoliTable) != expectedCRC {
		return nil, fmt.Errorf("derived index checksum mismatch")
	}

	return payload, nil
}
