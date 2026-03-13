package auth

const (
	RoleOwner     = "owner"
	RoleAdmin     = "admin"
	RoleOperator  = "operator"
	RoleDeveloper = "developer"
	RoleReadOnly  = "read_only"
	RoleBilling   = "billing"
	RoleUser      = "user" // legacy inference-only role
)

const (
	PrincipalHuman          = "human"
	PrincipalServiceAccount = "service_account"
)

const (
	PermissionDashboardAccess      = "dashboard_access"
	PermissionManageKeys           = "manage_keys"
	PermissionManageMemberships    = "manage_memberships"
	PermissionManageWorkspaces     = "manage_workspaces"
	PermissionManageQuotas         = "manage_quotas"
	PermissionViewUsage            = "view_usage"
	PermissionViewInfrastructure   = "view_infrastructure"
	PermissionManageInfrastructure = "manage_infrastructure"
	PermissionManageVault          = "manage_vault"
)

func IsValidRole(role string) bool {
	switch role {
	case RoleOwner, RoleAdmin, RoleOperator, RoleDeveloper, RoleReadOnly, RoleBilling, RoleUser:
		return true
	default:
		return false
	}
}

func IsValidPrincipalType(principalType string) bool {
	switch principalType {
	case PrincipalHuman, PrincipalServiceAccount:
		return true
	default:
		return false
	}
}

func HasPermission(record *KeyRecord, permission string) bool {
	if record == nil || record.Status != "active" {
		return false
	}

	switch permission {
	case PermissionDashboardAccess:
		return record.PrincipalType == PrincipalHuman && record.Role != RoleUser
	case PermissionManageKeys:
		return record.Role == RoleOwner || record.Role == RoleAdmin
	case PermissionManageMemberships:
		return record.Role == RoleOwner || record.Role == RoleAdmin
	case PermissionManageWorkspaces:
		return record.Role == RoleOwner || record.Role == RoleAdmin
	case PermissionManageQuotas:
		return record.Role == RoleOwner || record.Role == RoleAdmin || record.Role == RoleBilling
	case PermissionViewUsage:
		return record.Role == RoleOwner || record.Role == RoleAdmin || record.Role == RoleBilling || record.Role == RoleReadOnly
	case PermissionViewInfrastructure:
		return record.Role == RoleOwner || record.Role == RoleAdmin || record.Role == RoleOperator || record.Role == RoleReadOnly
	case PermissionManageInfrastructure:
		return record.Role == RoleOwner || record.Role == RoleAdmin || record.Role == RoleOperator
	case PermissionManageVault:
		return record.Role == RoleOwner || record.Role == RoleAdmin
	default:
		return false
	}
}

func CanCreateSession(record *KeyRecord) bool {
	return HasPermission(record, PermissionDashboardAccess)
}

func CanAssignRole(actor *KeyRecord, targetRole string) bool {
	if actor == nil || !IsValidRole(targetRole) {
		return false
	}
	switch actor.Role {
	case RoleOwner:
		return targetRole != RoleOwner || actor.WorkspaceID == DefaultWorkspaceID
	case RoleAdmin:
		return targetRole != RoleOwner && targetRole != RoleAdmin
	default:
		return false
	}
}
