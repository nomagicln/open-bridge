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
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

const (
	// DerivedIndexSchemaVersion is bumped whenever the DerivedIndex DTO fields or
	// their semantics change in a way that invalidates persisted indexes.
	DerivedIndexSchemaVersion = 4
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
	// indexDescriptionMaxLength is the maximum number of runes persisted for
	// descriptions; command help does not need the full text.
	indexDescriptionMaxLength = 200
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

// DerivedOperation is the operation summary needed by application help and
// command-level flag completion.
type DerivedOperation struct {
	Verb        string
	Method      string
	Path        string
	OperationID string
	Summary     string
	Description string
	// DescriptionTruncated reports that Description was trimmed to
	// indexDescriptionMaxLength runes; command help falls back to parsing when
	// the full description is required.
	DescriptionTruncated bool
	Resource             string
	Aliases              []string
	Tags                 []string
	Parameters           []DerivedParameter
	BodySchema           *DerivedSchema
	BodyRequired         bool
	BodyMediaType        string
	HasBody              bool
	// ExecutionIndexable reports that the operation can be reconstructed from
	// the index for the CLI execution path without parsing the full spec.
	ExecutionIndexable bool
	// ExecutionBlockReason explains why the operation is not executable from the
	// index (empty when ExecutionIndexable is true).
	ExecutionBlockReason string
}

// DerivedParameter is the parameter subset needed by command help, flag
// completion and the execution fast path.
type DerivedParameter struct {
	Name                 string
	In                   string
	Required             bool
	Description          string
	DescriptionTruncated bool
	HasExample           bool
	Example              string
	Schema               *DerivedSchema
}

// DerivedSchema is a recursive, gob-encodable OpenAPI schema subset. It is
// explicit (never a gob-encoded openapi3.Schema) and is rebuilt recursively for
// both command help and the execution fast path.
type DerivedSchema struct {
	Type                        string
	Types                       []string
	Format                      string
	Enum                        []string
	EnumAreStrings              bool
	Description                 string
	DescriptionTruncated        bool
	HasExample                  bool
	Example                     string
	Required                    []string
	Properties                  map[string]*DerivedSchema
	Items                       *DerivedSchema
	AdditionalProperties        *DerivedSchema
	AdditionalPropertiesSet     bool
	AdditionalPropertiesAllowed bool
	OneOf                       []*DerivedSchema
	AnyOf                       []*DerivedSchema
	AllOf                       []*DerivedSchema
	Ref                         string
}

// derivedSchemaMaxDepth bounds recursive schema reconstruction.
const derivedSchemaMaxDepth = 16

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
		// The source may have been removed after the index was built. Fall back
		// to the stable identity entry so indexable commands keep working.
		if idx := p.loadIdentityDerivedIndex(source, appName); idx != nil {
			return idx, nil
		}

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
	populateDerivedOperationDetails(idx, doc)

	_ = p.indexCache.store(key, idx)

	return idx, nil
}

// loadIdentityDerivedIndex looks up the persisted index by its stable identity
// (app + source kind + source path) when the spec stat is unavailable.
func (p *Parser) loadIdentityDerivedIndex(source, appName string) *DerivedIndex {
	if p.indexCache == nil {
		return nil
	}

	absPath, err := filepath.Abs(source)
	if err != nil {
		return nil
	}

	base := derivedIndexKey{
		AppName:            appName,
		SourceKind:         "file",
		Source:             absPath,
		ParserVersion:      parserVersion(),
		IndexSchemaVersion: DerivedIndexSchemaVersion,
	}

	return p.indexCache.loadIdentity(base)
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
	populateDerivedOperationDetails(idx, doc)

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

// decodeDerivedIndexFile validates the container header and the embedded key,
// and returns the payload.
func decodeDerivedIndexFile(data []byte, expected derivedIndexKey) ([]byte, error) {
	actualKey, payload, err := decodeDerivedIndexFileHeader(data)
	if err != nil {
		return nil, err
	}

	if actualKey != expected {
		return nil, fmt.Errorf("derived index key mismatch")
	}

	return payload, nil
}

// decodeDerivedIndexFileHeader validates the container envelope (magic, format
// version, lengths, CRC32C) and decodes the embedded key. It does not compare
// the key with a caller expectation, which lets the loader fall back to the
// stable identity of an entry whose source file has disappeared.
func decodeDerivedIndexFileHeader(data []byte) (derivedIndexKey, []byte, error) {
	headerSize := derivedIndexMagicSize + 4*4
	if len(data) < headerSize {
		return derivedIndexKey{}, nil, fmt.Errorf("derived index file too short: %d bytes", len(data))
	}

	if !bytes.Equal(data[:derivedIndexMagicSize], derivedIndexMagic[:]) {
		return derivedIndexKey{}, nil, fmt.Errorf("invalid derived index magic")
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
		return derivedIndexKey{}, nil, fmt.Errorf("unsupported derived index format version %d", formatVersion)
	}

	if uint64(offset)+uint64(keyLen)+uint64(payloadLen) != uint64(len(data)) {
		return derivedIndexKey{}, nil, fmt.Errorf("derived index file has inconsistent lengths")
	}

	var key derivedIndexKey
	if err := json.Unmarshal(data[offset:offset+int(keyLen)], &key); err != nil {
		return derivedIndexKey{}, nil, fmt.Errorf("failed to decode derived index key: %w", err)
	}

	payload := data[offset+int(keyLen):]
	if crc32.Checksum(payload, castagnoliTable) != expectedCRC {
		return derivedIndexKey{}, nil, fmt.Errorf("derived index checksum mismatch")
	}

	return key, payload, nil
}

// loadIdentity reads an entry by its stable identity (app + kind + source),
// ignoring the volatile size/mtime/content-hash parts of the key. It is used
// when the local spec file has been removed but the index is still usable.
func (c *derivedIndexCache) loadIdentity(base derivedIndexKey) *DerivedIndex {
	path := c.path(base)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	headerKey, payload, err := decodeDerivedIndexFileHeader(data)
	if err != nil {
		_ = os.Remove(path)

		return nil
	}

	if headerKey.AppName != base.AppName ||
		headerKey.SourceKind != base.SourceKind ||
		headerKey.Source != base.Source ||
		headerKey.ParserVersion != base.ParserVersion ||
		headerKey.IndexSchemaVersion != base.IndexSchemaVersion {
		return nil
	}

	idx, err := unmarshalDerivedIndexPayload(payload)
	if err != nil {
		_ = os.Remove(path)

		return nil
	}

	if !indexMatchesKey(idx, headerKey) {
		_ = os.Remove(path)

		return nil
	}

	return idx
}

// trimIndexText trims s to indexDescriptionMaxLength runes, reporting whether it
// was truncated. The trim is rune-aware so multi-byte text stays valid UTF-8.
func trimIndexText(s string) (string, bool) {
	if len([]rune(s)) <= indexDescriptionMaxLength {
		return s, false
	}

	trimmed := string([]rune(s)[:indexDescriptionMaxLength])

	return trimmed, true
}

// populateDerivedOperationDetails enriches the persisted command tree with the
// parameter and request-body details needed to render command-level help.
func populateDerivedOperationDetails(idx *DerivedIndex, doc *openapi3.T) {
	if idx == nil || doc == nil {
		return
	}

	for i := range idx.Resources {
		populateDerivedResourceDetails(&idx.Resources[i], doc)
	}
}

// populateDerivedResourceDetails enriches one resource (and its sub-resources).
func populateDerivedResourceDetails(resource *DerivedResource, doc *openapi3.T) {
	for i := range resource.Operations {
		op := &resource.Operations[i]
		opSpec := operationSpecFromDoc(doc, op.Path, op.Method)
		if opSpec == nil {
			continue
		}

		op.applySpecDetails(opSpec)
	}

	for i := range resource.SubResources {
		populateDerivedResourceDetails(&resource.SubResources[i], doc)
	}
}

// applySpecDetails copies the help/execution-relevant operation details into the DTO.
func (op *DerivedOperation) applySpecDetails(opSpec *openapi3.Operation) {
	if op.Description != "" {
		op.Description, op.DescriptionTruncated = trimIndexText(op.Description)
	}

	params, paramReason := extractDerivedParameters(opSpec)
	op.Parameters = params

	bodySchema, bodyRequired, bodyMedia, hasBody, bodyReason := extractDerivedBody(opSpec)
	op.BodySchema = bodySchema
	op.BodyRequired = bodyRequired
	op.BodyMediaType = bodyMedia
	op.HasBody = hasBody

	reason := paramReason
	if reason == "" {
		reason = bodyReason
	}

	op.ExecutionIndexable = reason == ""
	op.ExecutionBlockReason = reason
}

// extractDerivedParameters converts OpenAPI parameters into the persisted form.
// It always builds the best-effort DTO (so help can still render) and returns
// the first reason why execution from the index would not be equivalent.
func extractDerivedParameters(opSpec *openapi3.Operation) ([]DerivedParameter, string) {
	if opSpec == nil || len(opSpec.Parameters) == 0 {
		return nil, ""
	}

	params := make([]DerivedParameter, 0, len(opSpec.Parameters))
	visited := make(map[*openapi3.Schema]bool)
	reason := ""

	for _, ref := range opSpec.Parameters {
		if ref == nil || ref.Value == nil {
			reason = firstNonEmptyReason(reason, "parameter reference is not resolved")
			continue
		}

		param := ref.Value
		derived := DerivedParameter{
			Name:     param.Name,
			In:       param.In,
			Required: param.Required,
		}

		if param.Description != "" {
			derived.Description, derived.DescriptionTruncated = trimIndexText(param.Description)
		}

		if param.Example != nil {
			derived.HasExample = true
			derived.Example = fmt.Sprintf("%v", param.Example)
		}

		if len(param.Content) > 0 {
			reason = firstNonEmptyReason(reason, "parameter uses a content schema")
		} else if param.Schema == nil || param.Schema.Value == nil {
			reason = firstNonEmptyReason(reason, "parameter has no schema")
		} else {
			schema, schemaReason := derivedSchemaFromRef(param.Schema, visited, 0)
			derived.Schema = schema
			reason = firstNonEmptyReason(reason, schemaReason)
		}

		params = append(params, derived)
	}

	return params, reason
}

// extractDerivedBody converts a request body schema into the persisted form.
func extractDerivedBody(opSpec *openapi3.Operation) (*DerivedSchema, bool, string, bool, string) {
	if opSpec == nil || opSpec.RequestBody == nil || opSpec.RequestBody.Value == nil {
		return nil, false, "", false, ""
	}

	body := opSpec.RequestBody.Value
	reason := ""

	if len(body.Content) != 1 {
		reason = "request body has multiple media types"
	}

	visited := make(map[*openapi3.Schema]bool)

	for media, content := range body.Content {
		if content == nil || content.Schema == nil || content.Schema.Value == nil {
			reason = firstNonEmptyReason(reason, "request body has no schema")

			return nil, body.Required, media, false, reason
		}

		schema, schemaReason := derivedSchemaFromRef(content.Schema, visited, 0)
		reason = firstNonEmptyReason(reason, schemaReason)

		if media != "application/json" {
			reason = firstNonEmptyReason(reason, "request body media type is not application/json")
		}

		return schema, body.Required, media, schema != nil, reason
	}

	return nil, body.Required, "", false, reason
}

// derivedSchemaFromRef converts a schema reference recursively. A non-empty
// reason means the schema cannot be used by the execution fast path.
func derivedSchemaFromRef(ref *openapi3.SchemaRef, visited map[*openapi3.Schema]bool, depth int) (*DerivedSchema, string) {
	if ref == nil || ref.Value == nil {
		return nil, "schema reference is not resolved"
	}

	return derivedSchemaFromSchema(ref.Value, ref.Ref, visited, depth)
}

// derivedSchemaFromSchema converts one resolved OpenAPI schema recursively.
func derivedSchemaFromSchema(schema *openapi3.Schema, refName string, visited map[*openapi3.Schema]bool, depth int) (*DerivedSchema, string) {
	if schema == nil {
		return nil, "schema is missing"
	}

	if depth > derivedSchemaMaxDepth {
		return nil, "schema nesting is too deep"
	}

	if visited[schema] {
		return nil, "recursive schema"
	}

	visited[schema] = true
	defer delete(visited, schema)

	derived := &DerivedSchema{Format: schema.Format, Ref: refName}
	applyDerivedSchemaBasics(derived, schema)

	reason := applyDerivedSchemaChildren(derived, schema, visited, depth)

	return derived, firstNonEmptyReason(reason, schemaExecutionReason(derived))
}

// applyDerivedSchemaBasics copies the scalar schema keywords.
func applyDerivedSchemaBasics(derived *DerivedSchema, schema *openapi3.Schema) {
	if schema.Type != nil {
		derived.Types = append([]string(nil), schema.Type.Slice()...)
		derived.Type = primarySchemaType(derived.Types)
	}

	derived.Enum, derived.EnumAreStrings = formatEnumValues(schema.Enum)

	if schema.Description != "" {
		derived.Description, derived.DescriptionTruncated = trimIndexText(schema.Description)
	}

	if schema.Example != nil {
		derived.HasExample = true
		derived.Example = fmt.Sprintf("%v", schema.Example)
	}

	derived.Required = append([]string(nil), schema.Required...)
}

// applyDerivedSchemaChildren converts the nested schema keywords.
func applyDerivedSchemaChildren(derived *DerivedSchema, schema *openapi3.Schema, visited map[*openapi3.Schema]bool, depth int) string {
	reason := ""

	derived.OneOf, reason = derivedSchemaList(schema.OneOf, visited, depth, reason)
	derived.AnyOf, reason = derivedSchemaList(schema.AnyOf, visited, depth, reason)
	derived.AllOf, reason = derivedSchemaList(schema.AllOf, visited, depth, reason)

	derived.AdditionalPropertiesSet, derived.AdditionalPropertiesAllowed, derived.AdditionalProperties, reason = derivedAdditionalProperties(schema, visited, depth, reason)

	derived.Properties = make(map[string]*DerivedSchema, len(schema.Properties))

	for name, propRef := range schema.Properties {
		child, childReason := derivedSchemaFromRef(propRef, visited, depth+1)
		derived.Properties[name] = child
		reason = firstNonEmptyReason(reason, childReason)
	}

	if len(derived.Properties) == 0 {
		derived.Properties = nil
	}

	if schema.Items != nil {
		items, itemsReason := derivedSchemaFromRef(schema.Items, visited, depth+1)
		derived.Items = items
		reason = firstNonEmptyReason(reason, itemsReason)
	}

	return reason
}

// derivedSchemaList converts a list of schemas (oneOf/anyOf/allOf).
func derivedSchemaList(refs []*openapi3.SchemaRef, visited map[*openapi3.Schema]bool, depth int, reason string) ([]*DerivedSchema, string) {
	if len(refs) == 0 {
		return nil, reason
	}

	derived := make([]*DerivedSchema, 0, len(refs))

	for _, ref := range refs {
		child, childReason := derivedSchemaFromRef(ref, visited, depth+1)
		derived = append(derived, child)
		reason = firstNonEmptyReason(reason, childReason)
	}

	return derived, reason
}

// derivedAdditionalProperties converts the additionalProperties keyword.
func derivedAdditionalProperties(schema *openapi3.Schema, visited map[*openapi3.Schema]bool, depth int, reason string) (set, allowed bool, derived *DerivedSchema, outReason string) {
	outReason = reason

	if schema.AdditionalProperties.Schema != nil {
		child, childReason := derivedSchemaFromRef(schema.AdditionalProperties.Schema, visited, depth+1)

		return true, true, child, firstNonEmptyReason(outReason, childReason)
	}

	if schema.AdditionalProperties.Has != nil {
		return true, *schema.AdditionalProperties.Has, nil, outReason
	}

	return false, false, nil, outReason
}

// schemaExecutionReason reports why a converted schema cannot drive the
// execution fast path (empty when it can).
func schemaExecutionReason(schema *DerivedSchema) string {
	if schema == nil {
		return "schema is missing"
	}

	if len(schema.AllOf) > 0 {
		return "schema uses allOf"
	}

	if schema.AdditionalPropertiesSet {
		return "schema has additionalProperties"
	}

	if len(schema.Types) == 0 {
		if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
			return ""
		}

		return "schema has no type"
	}

	if len(schema.Types) != 1 {
		return "schema uses a union type"
	}

	switch schema.Types[0] {
	case "string", "integer", "number", "boolean":
		if len(schema.Enum) > 0 && !schema.EnumAreStrings {
			return "schema has a non-string enum"
		}

		return ""
	case "object":
		for _, required := range schema.Required {
			if _, ok := schema.Properties[required]; !ok {
				return "required property is missing from properties"
			}
		}

		return ""
	case "array":
		if schema.Items == nil {
			return "array has no item schema"
		}

		return ""
	default:
		return "unsupported schema type " + schema.Types[0]
	}
}

// firstNonEmptyReason returns primary when it is non-empty, else fallback.
func firstNonEmptyReason(primary, fallback string) string {
	if primary != "" {
		return primary
	}

	return fallback
}

// SemanticOperation reconstructs the semantic operation metadata used by the
// execution path, so the fast path avoids rebuilding the command tree.
func (op *DerivedOperation) SemanticOperation() *semantic.Operation {
	if op == nil {
		return nil
	}

	return &semantic.Operation{
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
}

// formatEnumValues renders enum values with %v (matching command help output)
// and reports whether every value was a Go string (matching flag-value
// completion, which only offers string enums).
func formatEnumValues(values []any) ([]string, bool) {
	if len(values) == 0 {
		return nil, false
	}

	formatted := make([]string, 0, len(values))
	allStrings := true

	for _, value := range values {
		if _, ok := value.(string); !ok {
			allStrings = false
		}

		formatted = append(formatted, fmt.Sprintf("%v", value))
	}

	return formatted, allStrings
}

// primarySchemaType returns the first declared schema type, if any.
func primarySchemaType(types []string) string {
	if len(types) == 0 {
		return ""
	}

	return types[0]
}

// operationSpecFromDoc finds the OpenAPI operation for a method and path.
func operationSpecFromDoc(doc *openapi3.T, path, method string) *openapi3.Operation {
	if doc == nil || doc.Paths == nil {
		return nil
	}

	pathItem := doc.Paths.Find(path)
	if pathItem == nil {
		return nil
	}

	switch strings.ToUpper(method) {
	case "GET":
		return pathItem.Get
	case "POST":
		return pathItem.Post
	case "PUT":
		return pathItem.Put
	case "PATCH":
		return pathItem.Patch
	case "DELETE":
		return pathItem.Delete
	case "HEAD":
		return pathItem.Head
	case "OPTIONS":
		return pathItem.Options
	default:
		return nil
	}
}

// FindOperation looks up an operation by resource name and verb. Resource names
// are matched like the CLI does: URL-style names ("store/order") are normalized
// to kebab-case ("store-order"), and sub-resources are searched recursively.
func (d *DerivedIndex) FindOperation(resource, verb string) (*DerivedOperation, bool) {
	if d == nil {
		return nil, false
	}

	normalized := strings.ReplaceAll(resource, "/", "-")

	for i := range d.Resources {
		if op, ok := d.Resources[i].findOperation(normalized, verb); ok {
			return op, true
		}
	}

	return nil, false
}

// findOperation searches a resource and its sub-resources for a verb.
func (r *DerivedResource) findOperation(resource, verb string) (*DerivedOperation, bool) {
	if r.Name == resource {
		for i := range r.Operations {
			if r.Operations[i].Verb == verb {
				return &r.Operations[i], true
			}
		}
	}

	for i := range r.SubResources {
		if op, ok := r.SubResources[i].findOperation(resource, verb); ok {
			return op, true
		}
	}

	return nil, false
}

// CommandHelpIndexable reports whether command-level help can be rendered from
// this index entry with byte-identical output to the parsing path.
func (op *DerivedOperation) CommandHelpIndexable() bool {
	if op.Summary == "" && op.DescriptionTruncated {
		return false
	}

	for i := range op.Parameters {
		if op.Parameters[i].DescriptionTruncated {
			return false
		}
	}

	return !schemaHasTruncatedDescription(op.BodySchema)
}

// schemaHasTruncatedDescription reports whether any schema node was trimmed.
func schemaHasTruncatedDescription(schema *DerivedSchema) bool {
	if schema == nil {
		return false
	}

	if schema.DescriptionTruncated {
		return true
	}

	for _, prop := range schema.Properties {
		if schemaHasTruncatedDescription(prop) {
			return true
		}
	}

	if schemaHasTruncatedDescription(schema.Items) {
		return true
	}

	for _, list := range [][]*DerivedSchema{schema.OneOf, schema.AnyOf, schema.AllOf} {
		if slices.ContainsFunc(list, schemaHasTruncatedDescription) {
			return true
		}
	}

	return schemaHasTruncatedDescription(schema.AdditionalProperties)
}

// CommandSpec reconstructs a minimal *openapi3.Operation that renders command
// help byte-identically to the parsing path and carries the schema information
// needed by the execution fast path.
func (op *DerivedOperation) CommandSpec() *openapi3.Operation {
	return &openapi3.Operation{
		Summary:     op.Summary,
		Description: op.Description,
		Parameters:  op.commandParameters(),
		RequestBody: op.commandRequestBody(),
	}
}

// commandParameters reconstructs the OpenAPI parameter list.
func (op *DerivedOperation) commandParameters() openapi3.Parameters {
	if len(op.Parameters) == 0 {
		return nil
	}

	params := make(openapi3.Parameters, 0, len(op.Parameters))

	for _, param := range op.Parameters {
		reconstructed := &openapi3.Parameter{
			Name:        param.Name,
			In:          param.In,
			Required:    param.Required,
			Description: param.Description,
			Schema:      openAPISchemaRef(param.Schema),
		}

		if param.HasExample {
			reconstructed.Example = param.Example
		}

		params = append(params, &openapi3.ParameterRef{Value: reconstructed})
	}

	return params
}

// commandRequestBody reconstructs the OpenAPI request body, if any.
func (op *DerivedOperation) commandRequestBody() *openapi3.RequestBodyRef {
	if !op.HasBody || op.BodySchema == nil {
		return nil
	}

	mediaType := op.BodyMediaType
	if mediaType == "" {
		mediaType = "application/json"
	}

	body := &openapi3.RequestBody{
		Required: op.BodyRequired,
		Content: openapi3.Content{
			mediaType: &openapi3.MediaType{Schema: openAPISchemaRef(op.BodySchema)},
		},
	}

	return &openapi3.RequestBodyRef{Value: body}
}

// openAPISchemaRef rebuilds a schema reference from the persisted DTO.
func openAPISchemaRef(schema *DerivedSchema) *openapi3.SchemaRef {
	if schema == nil {
		return nil
	}

	return &openapi3.SchemaRef{Value: schema.openAPISchema()}
}

// openAPISchema recursively rebuilds an *openapi3.Schema from the DTO.
func (ds *DerivedSchema) openAPISchema() *openapi3.Schema {
	if ds == nil {
		return nil
	}

	schema := &openapi3.Schema{
		Format:      ds.Format,
		Description: ds.Description,
	}

	if len(ds.Types) > 0 {
		schema.Type = schemaTypes(ds.Types)
	}

	schema.Enum = enumValuesAsAny(ds.Enum)
	if ds.HasExample {
		schema.Example = ds.Example
	}

	schema.Required = append([]string(nil), ds.Required...)

	if len(ds.Properties) > 0 {
		schema.Properties = make(map[string]*openapi3.SchemaRef, len(ds.Properties))
		for name, prop := range ds.Properties {
			schema.Properties[name] = openAPISchemaRef(prop)
		}
	}

	schema.Items = openAPISchemaRef(ds.Items)

	if ds.AdditionalPropertiesSet {
		has := ds.AdditionalPropertiesAllowed
		schema.AdditionalProperties.Has = &has

		if ds.AdditionalProperties != nil {
			schema.AdditionalProperties.Schema = openAPISchemaRef(ds.AdditionalProperties)
		}
	}

	schema.OneOf = openAPISchemaList(ds.OneOf)
	schema.AnyOf = openAPISchemaList(ds.AnyOf)
	schema.AllOf = openAPISchemaList(ds.AllOf)

	return schema
}

// openAPISchemaList rebuilds a list of schema references.
func openAPISchemaList(schemas []*DerivedSchema) []*openapi3.SchemaRef {
	if len(schemas) == 0 {
		return nil
	}

	refs := make([]*openapi3.SchemaRef, 0, len(schemas))
	for _, schema := range schemas {
		refs = append(refs, openAPISchemaRef(schema))
	}

	return refs
}

// schemaTypes returns a pointer to a copy of types, or nil when empty.
func schemaTypes(types []string) *openapi3.Types {
	if len(types) == 0 {
		return nil
	}

	values := openapi3.Types(append([]string(nil), types...))

	return &values
}

// enumValuesAsAny converts pre-formatted enum strings back to []any. Command
// help renders values with %v, so the formatted string is byte-identical.
func enumValuesAsAny(values []string) []any {
	if len(values) == 0 {
		return nil
	}

	anyValues := make([]any, 0, len(values))
	for _, value := range values {
		anyValues = append(anyValues, value)
	}

	return anyValues
}

// FlagNames returns the --flag names derived from parameters and request body
// properties, restricted to prefix, sorted for deterministic completion output.
func (op *DerivedOperation) FlagNames(prefix string) []string {
	if op == nil {
		return nil
	}

	flags := make([]string, 0, len(op.Parameters)+len(op.bodyPropertyNames()))

	for _, param := range op.Parameters {
		flag := "--" + param.Name
		if strings.HasPrefix(flag, prefix) {
			flags = append(flags, flag)
		}
	}

	for _, name := range op.bodyPropertyNames() {
		flag := "--" + name
		if strings.HasPrefix(flag, prefix) {
			flags = append(flags, flag)
		}
	}

	sort.Strings(flags)

	return flags
}

// bodyPropertyNames returns the sorted request body property names.
func (op *DerivedOperation) bodyPropertyNames() []string {
	if op.BodySchema == nil || len(op.BodySchema.Properties) == 0 {
		return nil
	}

	names := make([]string, 0, len(op.BodySchema.Properties))
	for name := range op.BodySchema.Properties {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// EnumValues returns the string enum values for a flag, mirroring
// flag-value completion semantics. handled is true when the operation exists;
// callers should not fall back to parsing in that case.
func (op *DerivedOperation) EnumValues(flagName string) (values []string, handled bool) {
	if op == nil {
		return nil, false
	}

	for _, param := range op.Parameters {
		if param.Name == flagName {
			return enumValuesFromSchema(param.Schema), true
		}
	}

	if op.BodySchema != nil {
		if prop, ok := op.BodySchema.Properties[flagName]; ok {
			return enumValuesFromSchema(prop), true
		}
	}

	return nil, true
}

// enumValuesFromSchema returns sorted string enum values when the schema enum
// was declared as strings (matching the parsing path's enum completion).
func enumValuesFromSchema(schema *DerivedSchema) []string {
	if schema == nil {
		return nil
	}

	return sortedStringEnums(schema.Enum, schema.EnumAreStrings)
}

// sortedStringEnums returns a sorted copy of the enum values when they were
// declared as strings (matching the parsing path's enum completion).
func sortedStringEnums(values []string, areStrings bool) []string {
	if !areStrings || len(values) == 0 {
		return nil
	}

	sorted := append([]string(nil), values...)
	sort.Strings(sorted)

	return sorted
}

// ensure utf8 import is used for rune-aware trimming documentation helpers.
var _ = utf8.ValidString
