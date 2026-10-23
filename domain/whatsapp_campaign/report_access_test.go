package whatsapp_campaign

import "testing"

func TestAReportReadsACampaignOnlyInsideItsWorkspaceAndDepartments(t *testing.T) {
	allowSales := func(id string) bool { return id == "sales" }
	cases := []struct {
		name     string
		access   ReportAccess
		campaign *Campaign
		want     bool
	}{
		{"same workspace and an allowed department", ReportAccess{WorkspaceID: "ws", AllowDepartment: allowSales}, &Campaign{WorkspaceID: "ws", DepartmentID: "sales"}, true},
		{"another workspace", ReportAccess{WorkspaceID: "ws", AllowDepartment: allowSales}, &Campaign{WorkspaceID: "other", DepartmentID: "sales"}, false},
		{"a department out of scope", ReportAccess{WorkspaceID: "ws", AllowDepartment: allowSales}, &Campaign{WorkspaceID: "ws", DepartmentID: "support"}, false},
		{"no department rule fails closed", ReportAccess{WorkspaceID: "ws"}, &Campaign{WorkspaceID: "ws"}, false},
		{"no campaign", ReportAccess{WorkspaceID: "ws", AllowDepartment: allowSales}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.access.AllowsCampaign(tc.campaign); got != tc.want {
				t.Fatalf("AllowsCampaign = %v, want %v", got, tc.want)
			}
		})
	}
}
