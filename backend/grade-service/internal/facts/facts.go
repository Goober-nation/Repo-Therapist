// Package facts derives the few repository facts the public ranking can filter on (size, a CI
// config, a security policy file) from the exports Grade already reads. They are plain observations,
// never a score: a fact that could not be established is nil ("unknown"), which a filter never
// treats as "no".
package facts

// Inputs are what Grade read for the repository.
type Inputs struct {
	// CodeAvailable is false when the code export was unreadable: size and the security policy
	// are then unknown.
	CodeAvailable bool
	Files, LOC    int
	// CIAvailable is false when ci-runs.json was missing or unreadable; CIConfigFiles are the CI
	// configs found in the repository.
	CIAvailable   bool
	CIConfigFiles []string
	// SecurityPolicyFound: a SECURITY.md (or equivalent) exists in the repository.
	SecurityPolicyFound bool
}

// Facts is the grading.results "facts" object. Nil fields are unknown.
type Facts struct {
	Files *int `json:"files"`
	LOC   *int `json:"loc"`
	// HasCI: a CI config file exists. It says a pipeline is configured, not that it passes: run
	// results are readable only by the repository's owner.
	HasCI *bool `json:"has_ci"`
	// HasSecurityPolicy: a SECURITY.md exists. It is not an AppSec scan result, which only the
	// owner can read.
	HasSecurityPolicy *bool `json:"has_security_policy"`
}

// Build turns the inputs into facts, leaving unknown what the inputs cannot establish.
func Build(in Inputs) Facts {
	var out Facts

	if in.CodeAvailable {
		files, loc := in.Files, in.LOC
		out.Files, out.LOC = &files, &loc

		policy := in.SecurityPolicyFound
		out.HasSecurityPolicy = &policy
	}

	if in.CIAvailable {
		hasCI := len(in.CIConfigFiles) > 0
		out.HasCI = &hasCI
	}

	return out
}
