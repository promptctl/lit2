package config

// Schema is .lit/workspace.toml as this reader sees it, the one typed value
// tick, the enforcer and the field table judge state under (§3.15, §5 step
// 2). It has no keys yet: this binary does not parse the workspace file.
type Schema struct{}
