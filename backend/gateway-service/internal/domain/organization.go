package domain

// UserOrganization is one organization the PAT owner belongs to.
type UserOrganization struct {
	Slug        string
	DisplayName string
	// Personal marks the owner's own (personal) organization.
	Personal bool
}

// GetMyOrgsDTO is the PAT owner's org list plus the org to open first.
type GetMyOrgsDTO struct {
	Organizations  []UserOrganization
	DefaultOrgSlug string
}
