package auth

import "testing"

func TestCanCreateSessionMatchesHumanDashboardPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		role          string
		principalType string
		status        string
		want          bool
	}{
		{name: "owner", role: RoleOwner, principalType: PrincipalHuman, status: "active", want: true},
		{name: "admin", role: RoleAdmin, principalType: PrincipalHuman, status: "active", want: true},
		{name: "operator", role: RoleOperator, principalType: PrincipalHuman, status: "active", want: true},
		{name: "developer", role: RoleDeveloper, principalType: PrincipalHuman, status: "active", want: true},
		{name: "read only", role: RoleReadOnly, principalType: PrincipalHuman, status: "active", want: true},
		{name: "billing", role: RoleBilling, principalType: PrincipalHuman, status: "active", want: true},
		{name: "legacy inference only", role: RoleUser, principalType: PrincipalHuman, status: "active", want: false},
		{name: "service account", role: RoleOperator, principalType: PrincipalServiceAccount, status: "active", want: false},
		{name: "inactive human", role: RoleOperator, principalType: PrincipalHuman, status: "revoked", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			record := &KeyRecord{
				Role:          tt.role,
				PrincipalType: tt.principalType,
				Status:        tt.status,
			}
			if got := CanCreateSession(record); got != tt.want {
				t.Fatalf("CanCreateSession() = %v, want %v", got, tt.want)
			}
		})
	}
}
