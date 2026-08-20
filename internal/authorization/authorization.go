// Package authorization defines the small, centralized MVP capability policy.
package authorization

type Capability string

const (
	ManageUsers      Capability = "manage_users"
	ManageAppearance Capability = "manage_appearance"
	ManagePages      Capability = "manage_pages"
	ManagePosts      Capability = "manage_posts"
	ManageMedia      Capability = "manage_media"
	ManageMenus      Capability = "manage_menus"
	ManageRedirects  Capability = "manage_redirects"
	PublishContent   Capability = "publish_content"
)

func Allows(role string, capability Capability) bool {
	switch role {
	case "administrator":
		return true
	case "editor":
		switch capability {
		case ManagePages, ManagePosts, ManageMedia, ManageMenus, PublishContent:
			return true
		}
	case "author":
		return capability == ManagePosts || capability == ManageMedia
	}
	return false
}
