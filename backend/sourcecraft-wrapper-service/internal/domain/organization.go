package domain

// UserOrganization is one entry of GET /me/orgs.
type UserOrganization struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	// Personal is set by the wrapper, not by SourceCraft: the org whose slug equals the
	// caller's GET /user username.
	Personal bool `json:"-"`
}

// MyOrganizations is the caller's org list plus the org the UI should open first.
type MyOrganizations struct {
	Organizations  []UserOrganization
	DefaultOrgSlug string
}
