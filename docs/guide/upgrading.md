# Upgrading OpenBridge

The latest published tag is **v0.1.3**; `go install github.com/nomagicln/open-bridge/cmd/ob@latest` currently selects it. `main` and commit snapshots after that tag contain **Unreleased** changes. There is no published release for the runtime fixes described here. Pin the exact tag or commit you have tested.

Read the [CHANGELOG](https://github.com/nomagicln/open-bridge/blob/main/CHANGELOG.md), [CLI reference](/reference/cli), and [architecture reference](/reference/architecture) for the target ref. The [architecture audit](/maintenance/architecture-audit) tracks validation of this iteration; an Unreleased entry is not evidence that all checks have passed.

## Identify the consumer and the refs

Record the installed binary's `ob --version`, the Go module version where applicable, and the resolved old and target commits. Find your own command invocations, MCP tool names, YAML readers/writers, and Go imports before editing them. Compare the upstream refs and your local callsites together.

| Consumption mode | Check before upgrading |
| --- | --- |
| CLI or subprocess | Invocation form, profile/output flags, argument separators, generated snippets, exit behavior, and the installed spec's help |
| MCP client | Transport, profile safety policy, discovered tool names and schemas, confirmation flow, cancellation, and an actual representative call |
| Configuration | Config directory, app/profile names, credentials backend, custom headers/query parameters, TLS, and any custom serializer |
| Go imports | Removed exported symbols, constructor/options contracts, and the downstream project's own build and tests |

For reproducible inspection, run these in an OpenBridge checkout. Substitute the refs you actually consume:

```bash
git rev-parse v0.1.3
git rev-parse 6378155
git diff v0.1.3..6378155 -- pkg/config
```

The Go migration table below is scoped to **`git diff v0.1.3..6378155 -- pkg/config`**, where `6378155` means `6378155ed1e7c67733534c35ee257d961e9446a3`. It documents deletions already present at that baseline. Compare `6378155..<YOUR_TARGET_REF>` as well for later work; this table is not a promise about every future commit. It uses source declarations rather than export counts.

## Go source migration from v0.1.3

`pkg/config` now contains configuration models, validation, directory selection, and persistence. Installation belongs to `internal/install` inside this repository. Go's `internal` import rule prevents downstream modules from using it: **it is not a public replacement API**.

| Deleted v0.1.3 API or family | Supported approach and compatibility limit |
| --- | --- |
| `Manager.InstallApp`, `UpdateApp`, `UninstallApp`; `InstallOptions`, `InstallResult`, `InstalledAppInfo`, `GetInstalledAppInfo` | No public Go replacement. Installation automation can execute `ob install <app> --spec <source>`, inspect with `ob info <app> -o yaml` or `ob list --details`, and remove with `ob uninstall <app>`. `--force` overwrites existing configuration; use it only when that overwrite is intended. Pin v0.1.3 if the in-process Go contract is required. |
| `Manager.CreateShim`, `RemoveShim`, `ShimExists`; `IsShimDirInPath`, `GetPathInstructions` | No public Go replacement for shim management. Use install's `--shim` and uninstall's `--remove-shim` options for CLI automation, or retain the older tag. |
| `PathValidationError`, `TLSConfigValidation`, `ValidateAndResolvePath`, `ValidateTLSCertFile`, `ValidateTLSKeyFile`, `ValidateTLSCACertFile`, `ValidateTLSConfigPaths`, `IsValidPath` | No public replacement for these validation helpers. CLI installation owns its asset validation. Downstream Go code must retain its own path/TLS validation or pin the older API. Do not import `internal/install`. |
| `ProfileManager`, `Manager.NewProfileManager`; profile create/get/update/delete/default/list methods; `ProfileOptions`, `ProfileInfo`, `ProfileExistsError` | Load with `Manager.GetAppConfig`; use `NewProfile`, `AppConfig.AddProfile`, `GetProfile`, `DeleteProfile`, `SetDefaultProfile`, `GetDefaultProfile`, or `ListProfiles` as appropriate, then `Manager.SaveAppConfig`. These model methods do not reproduce every old validation rule or error type. See the write-back example below. |
| `ProfileManager.Reload`, `Save`, `RenameProfile`, `CopyProfile`, `ProfileExists`, `ProfileCount`, `SelectProfile`, `ListProfilesWithInfo`; `Manager.SelectProfileForApp`, `GetProfileNames`; `ProfileSummary` | Reload/save via `Manager.GetAppConfig`/`SaveAppConfig`; names via `AppConfig.ListProfiles`; existence via `GetProfile`; count via `len(app.Profiles)`. Explicit/default lookup can use `GetProfile`/`GetDefaultProfile`. Rename, copy, selection policy, rich summaries, and their old error/default semantics have no equivalent public convenience API. Preserve your own requirements or pin the older tag. |
| `ProfileManager.SetProfileHeader`, `DeleteProfileHeader`, `SetProfileQueryParam`, `ConfigureSafety`, `ConfigureTLS`, `ConfigureRetry` | Edit the selected `config.Profile` fields, write the value back with `AppConfig.AddProfile(*profile)`, validate, and persist. Preserve existing map entries and security settings. |
| `Manager.ExportProfile`, `ImportProfile`, `ProfileExport`; all option/file/bulk variants in `export.go`, including `ExportProfileWithOptions`, `ImportProfileWithOptions`, `ValidateImport`, `ExportAllProfiles`, `ExportAllProfilesAsSingle`, `ImportBulkProfiles`, `ExportProfileToFile`, `ImportProfileFromFile`, `ExportFormat`, `ExportOptions`, `ImportOptions`, `ImportValidationResult`, `ProfileExportV2`, `ExportedProfile`, `ExportedAuth`, `ExportMetadata`, `BulkExport`, `DefaultExportOptions` | No public replacement or profile import/export CLI. `ob info -o yaml/json` is configuration inspection and redacts selected sensitive headers; it is not the historical interchange format or a credential backup. Retain the older tag if your workflow needs those envelopes. |
| `SpecCacheManager`, its constructors/fetch/refresh/metadata/clear/info methods; `SpecCacheMeta`, `FetchResult`, `CacheInfo` | No drop-in public replacement for the old cache-management API or layout. For spec loading, use `pkg/spec.NewParser` with `WithRemoteCache` and, when needed, `WithIndexCache`, then `LoadSpecWithContext` or `LoadSpecWithOptions`. This provides parser caching, not the removed app-oriented management API. Pin the older tag if you need that API. |
| `config.SpecFetchOptions`, `Profile.BuildSpecFetchOptions` | `pkg/spec.SpecFetchOptions` and `Parser.LoadSpecWithOptions` support authenticated spec fetching. Construct the options explicitly from your intended headers/auth and retrieve secrets through the credential backend; there is no public profile-to-options helper replacement. |
| `SpecWatcher`, `SpecWatcherManager`, `LocalFileWatcher`, `RemoteSpecWatcher`; their constructors/options/events (`SpecChangeEvent`, `SpecChangeType`, `SpecChangeHandler`, `WatchMode`, `WithPollInterval`, `WithCheckInterval`, `WithRefreshBefore`) and lifecycle/check/handler methods | No public replacement. Parser cache revalidation on load does not provide background watching, callbacks, or the old watcher lifecycle. Pin the older tag if those contracts are required. |

### Profile updates must be written back

`AppConfig.GetProfile` returns a pointer to a **copy of the profile value**. Changing a scalar such as `BaseURL` does not update the value in `app.Profiles`. Map fields still reference their underlying maps; clone those maps if you need an independent copy. Always write an edited profile back, save the app, and verify by reloading it.

This example is a downstream helper using the existing public API:

```go
package integration

import (
    "fmt"

    "github.com/nomagicln/open-bridge/pkg/config"
)

func updateBaseURL(manager *config.Manager, appName, profileName, baseURL string) error {
    app, err := manager.GetAppConfig(appName)
    if err != nil {
        return fmt.Errorf("load app: %w", err)
    }
    profile, ok := app.GetProfile(profileName)
    if !ok {
        return fmt.Errorf("profile %q does not exist", profileName)
    }
    profile.Name = profileName // YAML profiles may omit the embedded name.
    profile.BaseURL = baseURL
    app.AddProfile(*profile)
    if err := config.ValidateConfig(app); err != nil {
        return fmt.Errorf("validate app: %w", err)
    }
    if err := manager.SaveAppConfig(app); err != nil {
        return fmt.Errorf("save app: %w", err)
    }

    saved, err := manager.GetAppConfig(appName)
    if err != nil {
        return fmt.Errorf("reload app: %w", err)
    }
    actual, ok := saved.GetProfile(profileName)
    if !ok || actual.BaseURL != baseURL {
        return fmt.Errorf("saved profile %q did not retain base URL", profileName)
    }
    return nil
}
```

Creating a profile uses `app.AddProfile(config.NewProfile(name, baseURL))`. `AddProfile` can overwrite an existing entry, so check existence when your workflow requires create-only behavior. Apply your own deterministic default-profile policy when removing or renaming profiles.

## Configuration and caches

Existing app YAML remains readable. Optional `spec_title` and `spec_version` fields were added to persist API metadata; they do not require rewriting old files. `version` is the app configuration version, `spec_version` is the API's version, and neither identifies an OpenBridge release.

App YAML and the old profile export formats are different contracts. The old `ProfileExport` envelope used `version`, `app_name`, `profile_name`, `profile`, and `exported_at`; `ProfileExportV2`/`BulkExport` used their own versioned envelopes and export options. Do not rename an export file to `apps/<app>.yaml` or treat `ob info` output as a drop-in importer.

Keep the configuration directory and credential backend intact. `OPENBRIDGE_CONFIG_DIR` overrides the entire config location; Linux/Unix otherwise respects `XDG_CONFIG_HOME`. Check which location each old/new process uses before concluding that apps disappeared.

Unreleased effective-parameter fixes advance the derived index schema from **4 to 5**. Old indexes rebuild automatically on load. No app configuration or credential deletion is required. The remote `spec-cache` may be your only offline copy of a spec; remove it only when the source can be recovered. See [cache lifecycle](/reference/architecture#caches-and-versions).

## Unreleased runtime behavior changes

These are the approved contracts for the current iteration, not v0.1.3 behavior. Consult the [architecture audit](/maintenance/architecture-audit) for implementation and test status.

- **MCP tool identity:** standard and progressive modes build one authorized registry after read-only/allow/deny filtering. Non-ambiguous names stay unchanged. If two allowed operations still produce the same name, registry construction fails with method/path/operationId diagnostics. Give the spec clean, unique `operationId` values supported by the existing naming rules, rediscover tools, and update client references. No new naming extension or automatic suffix is introduced.
- **Effective parameters:** path-level and operation-level parameters merge by `(in, name)`; an operation-level declaration overrides the same pair. CLI help/execution, completion, derived indexes, and both MCP modes use the merged result.
- **Request validation and profiles:** raw JSON bodies are validated as the body actually sent. Invalid JSON or missing required data fails before HTTP execution. MCP validates parameters and applies the CLI's profile layering: build request parameters, inject credentials, then apply profile `headers` and `query_params`. Profile values override the same header/query keys; unrelated values remain.
- **Entrypoints and flags:** `ob <app>`, `ob run <app>`, and the shim use one runtime path. Explicit app flags override explicit root defaults. Within one scope, the last repeated value wins; `--output` retains priority over `--json`/`--yaml`. The first `--` ends runtime options; subsequent tokens belong to the request. Missing runtime option values fail clearly.
- **Cancellation:** tool-call context reaches the HTTP request, retry waits can be cancelled, and SSE shuts down when the server context ends. Existing idempotent retry restrictions and safety policies remain in force.

Use the [CLI reference](/reference/cli) for supported flags. Run downstream builds/tests and representative calls with an isolated config and test credentials. Record blockers instead of deleting configuration, weakening safety settings, or silently substituting a different consumption mode.

## Pinning or rolling back

Install the previous immutable release binary or pin the Go module to a tested tag. For example:

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@v0.1.3
# In a downstream Go module, when v0.1.3 is the required API contract:
go get github.com/nomagicln/open-bridge@v0.1.3
```

Preserve your configuration and credentials, then run that consumer's own checks. Published tags remain immutable; rollback does not delete or move them. A copyable downstream agent prompt is in the [README](https://github.com/nomagicln/open-bridge/blob/main/README.md#upgrading-with-an-ai-agent).
