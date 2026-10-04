package domain

// UserOrganization is one organization the caller belongs to.
type UserOrganization struct {
	Slug        string
	DisplayName string
	// Personal marks the caller's own (personal) organization.
	Personal bool
}

// MyOrganizations is the caller's org list plus the org the UI should open first.
type MyOrganizations struct {
	Organizations  []UserOrganization
	DefaultOrgSlug string
}
