// security-wrapper — phase 4, not yet implemented. See backlog/phase-4-security-wrapper.md.
//
// Until this exists, repository-service (backend/repository-service/internal/service) calls the
// SourceCraft REST API directly for pulls/releases as a placeholder. Once this wrapper is real,
// that direct-call code should be swapped to go through it instead.
package main

import "log"

func main() {
	log.Fatal("security-wrapper: not yet implemented (see backlog/phase-4-security-wrapper.md)")
}
