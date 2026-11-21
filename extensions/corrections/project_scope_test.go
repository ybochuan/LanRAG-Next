package corrections

import "testing"

func TestProjectScopeUsesWholeName(t *testing.T) {
	for _, tc := range []struct {
		project, question string
		want              bool
	}{
		{"Project A", "What applies to Project Alpha?", false},
		{"Apollo", "What applies to Apollo2?", false},
		{"Apollo", "What applies to Apollo-2?", false},
		{"Apollo", "What applies to Apollo_2?", false},
		{"Apollo", "Does NanoApollo apply?", false},
		{"Project A", "What applies to Project A?", true},
		{"Apollo", "What applies to Apollo?", true},
		{"松桥项目", "请问松桥项目的报销期限", true},
		{"松桥项目", "大松桥项目报销期限", false},
		{"松桥项目", "松桥项目二期的报销期限", false},
		{"松桥项目", "排除松桥项目的报销期限", false},
	} {
		t.Run(tc.question, func(t *testing.T) {
			if got := appliesToProject(tc.project, tc.question); got != tc.want {
				t.Fatalf("project %q, applies=%v, want %v", tc.project, got, tc.want)
			}
		})
	}
}
