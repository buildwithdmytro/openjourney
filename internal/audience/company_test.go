package audience

import (
	"strings"
	"testing"
)

func TestCompanyCompileIsScopedAndParameterized(t *testing.T) {
	sql, args, err := CompileProfile(&Company{Field: "industry", Operator: "equals", Value: "software"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "company_members") || !strings.Contains(sql, "c.tenant_id=$1") || !strings.Contains(sql, "c.workspace_id=$2") {
		t.Fatalf("company SQL is not scoped: %s", sql)
	}
	if strings.Contains(sql, "software") || len(args) != 1 || args[0] != "software" {
		t.Fatalf("company value was not bound: sql=%s args=%#v", sql, args)
	}
}

func TestCompanyCompileRejectsUnsafeField(t *testing.T) {
	if _, _, err := CompileProfile(&Company{Field: "industry->>'x'", Operator: "equals", Value: "software"}); err == nil {
		t.Fatal("unsafe company field accepted")
	}
}
