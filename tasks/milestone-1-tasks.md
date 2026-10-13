# Milestone 1 Task List: Config Parser & Schema Validation

## Specification Summary
- **Original Requirements**: Implement Go structs matching the YAML spec (`sentinels.yaml`) with validation for timeouts, intervals, and required fields.
- **Module**: `github.com/GeorgeDeveloperJ/Beelzebub`
- **Stack**: Go 1.24+, `gopkg.in/yaml.v3`

## Development Tasks

### [x] Task 1: Go Module Initialization & Dependency Management
- **Goal**: Initialize `go.mod` with path `github.com/GeorgeDeveloperJ/Beelzebub` and add `gopkg.in/yaml.v3`.
- **Deliverables**: `go.mod`, `go.sum`.
- **Acceptance Criteria**: `go vet ./...` passes.

### [x] Task 2: Config Schema Definition & Deserialization
- **Goal**: Define Go struct models with YAML tags matching `sentinels.yaml` in `pkg/config/config.go`.
- **Details**:
  - Support human-friendly duration strings (`30s`, `15m`, `1h`) via custom YAML unmarshaling into standard `time.Duration`.
  - Parse `Config`, `GlobalConfig`, `NotificationsConfig`, `SentinelConfig`, `ProbeConfig`, `RemediationConfig`.
  - Provide `LoadFromFile(path string) (*Config, error)` and `ParseYAML(data []byte) (*Config, error)`.
- **Deliverables**: `pkg/config/config.go`.
- **Acceptance Criteria**: Valid YAML unmarshals cleanly without type panics.

### [x] Task 3: Comprehensive Config Validation Engine
- **Goal**: Implement deterministic semantic validation in `pkg/config/validate.go`.
- **Invariants**:
  - `Timeout` must be > 0 and strictly < `Interval`.
  - `Interval` must be >= 1s.
  - Sentinel names must be non-empty and alphanumeric with dashes/underscores (`^[a-zA-Z0-9_-]+$`).
  - Sentinel names must be unique across the fleet.
  - Probe `Type` must be either `"shell"` or `"threshold"`.
  - If Probe `Type == "threshold"`, `MinThreshold` must be specified.
  - Probe `Command` cannot be whitespace or empty.
  - Remediation `MaxRetries` must be >= 1 and `Command` non-empty if remediation is defined.
  - Notification `Provider` must be one of `["ntfy", "discord", "telegram"]`.
- **Deliverables**: `pkg/config/validate.go`.
- **Acceptance Criteria**: Returns structured multi-error reporting all violations clearly.

### [x] Task 4: Unit Test Suite & QA Verification
- **Goal**: Implement table-driven unit tests in `pkg/config/config_test.go`.
- **Coverage**:
  - Sample `sentinels.yaml` from `SPECIFICATIONS.md`.
  - Invalid YAML syntax.
  - Overlapping timeout (`timeout >= interval`).
  - Blank probe command.
  - Missing required fields.
  - Duplicate sentinel names.
  - Unsupported notification providers.
- **Deliverables**: `pkg/config/config_test.go`.
- **Acceptance Criteria**: `go test -v -race ./pkg/config/...` passes 100%.
